/**
* All tests related to 'gitops add' will go into this file
 */

package acceptance

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/weaveworks/weave-gitops/pkg/gitproviders"
)

var clusterName string

var _ = Describe("Weave GitOps App Add Tests", func() {
	deleteWegoRuntime := false
	if os.Getenv("DELETE_WEGO_RUNTIME_ON_EACH_TEST") == "true" {
		deleteWegoRuntime = true
	}

	var _ = BeforeEach(func() {
		By("Given I have a brand new cluster", func() {
			var err error

			_, err = ResetOrCreateCluster(WEGO_DEFAULT_NAMESPACE, deleteWegoRuntime)
			Expect(err).ShouldNot(HaveOccurred())

			clusterName = getClusterName()
		})

		By("And I have a gitops binary installed on my local machine", func() {
			Expect(FileExists(WEGO_BIN_PATH)).To(BeTrue())
		})
	})

	It("Verify that gitops cannot work without gitops components installed OR with both url and directory provided", func() {
		var repoAbsolutePath string
		var errOutput string
		var exitCode int
		private := true
		tip := generateTestInputs()
		appRepoRemoteURL := "ssh://git@github.com/" + GITHUB_ORG + "/" + tip.appRepoName + ".git"

		addCommand1 := "app add . --auto-merge=true"
		addCommand2 := "app add . --url=" + appRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And Gitops runtime is not installed", func() {
			uninstallWegoRuntime(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops add command", func() {
			command := exec.Command("sh", "-c", fmt.Sprintf("cd %s && %s %s", repoAbsolutePath, WEGO_BIN_PATH, addCommand1))
			session, err := gexec.Start(command, GinkgoWriter, GinkgoWriter)
			Expect(err).ShouldNot(HaveOccurred())
			Eventually(session).Should(gexec.Exit())
			exitCode = session.Wait().ExitCode()
		})

		By("Then I should see relevant message in the console", func() {
			// Should  be a failure
			Eventually(exitCode).ShouldNot(Equal(0))
		})

		By("When I run add command with both directory path and url specified", func() {
			_, errOutput = runWegoAddCommandWithOutput(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see an error", func() {
			Expect(errOutput).To(ContainSubstring("you should choose either --url or the app directory"))
		})
	})

	It("Verify that gitops does not modify the cluster when run with --dry-run flag", func() {
		var repoAbsolutePath string
		var addCommandOutput string
		private := true
		tip := generateTestInputs()
		branchName := "test-branch-01"
		appRepoRemoteURL := "ssh://git@github.com/" + GITHUB_ORG + "/" + tip.appRepoName + ".git"
		appName := tip.appRepoName
		appType := "Kustomization"

		addCommand := "app add --url=" + appRepoRemoteURL + " --branch=" + branchName + " --dry-run" + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I create a new branch", func() {
			createGitRepoBranch(repoAbsolutePath, branchName)
		})

		By("And I run 'gitops app add dry-run' command", func() {
			addCommandOutput, _ = runWegoAddCommandWithOutput(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see dry-run output with summary: name, url, path, branch and type", func() {
			Eventually(addCommandOutput).Should(MatchRegexp(`Name: ` + appName))
			Eventually(addCommandOutput).Should(MatchRegexp(`URL: ` + appRepoRemoteURL))
			Eventually(addCommandOutput).Should(MatchRegexp(`Path: ./`))
			Eventually(addCommandOutput).Should(MatchRegexp(`Branch: ` + branchName))
			Eventually(addCommandOutput).Should(MatchRegexp(`Type: kustomize`))

			Eventually(addCommandOutput).Should(MatchRegexp(`✚ Generating Source manifest`))
			Eventually(addCommandOutput).Should(MatchRegexp(`✚ Generating GitOps automation manifests`))
			Eventually(addCommandOutput).Should(MatchRegexp(`✚ Generating Application spec manifest`))
			Eventually(addCommandOutput).Should(MatchRegexp(`► Applying manifests to the cluster`))

			Eventually(addCommandOutput).Should(MatchRegexp(
				`apiVersion:.*\nkind: GitRepository\nmetadata:\n\s*name: ` + appName + `\n\s*namespace: ` + WEGO_DEFAULT_NAMESPACE + `[a-z0-9:\n\s*]+branch: ` + branchName + `[a-zA-Z0-9:\n\s*-]+url: ` + appRepoRemoteURL))

			Eventually(addCommandOutput).Should(MatchRegexp(
				`apiVersion:.*\nkind: ` + appType + `\nmetadata:\n\s*name: ` + appName + `-apps-dir\n\s*namespace: ` + WEGO_DEFAULT_NAMESPACE))
		})

		By("And I should not see any workload deployed to the cluster", func() {
			verifyWegoAddCommandWithDryRun(tip.appRepoName, WEGO_DEFAULT_NAMESPACE)
		})
	})

	It("Verify that gitops can deploy an app after it is setup with an empty repo initially", func() {
		var repoAbsolutePath string
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName

		addCommand := "app add . --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create an empty private repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I git add-commit-push app workload to repo", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I should see workload is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have private visibility", func() {
			Expect(getGitRepoVisibility(GITHUB_ORG, tip.appRepoName, gitproviders.GitProviderGitHub)).Should(ContainSubstring("private"))
		})
	})

	It("Verify that gitops can deploy and remove a gitlab app after it is setup with an empty repo initially", func() {
		var repoAbsolutePath string
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		var appRemoveOutput *gexec.Session

		addCommand := "app add . --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITLAB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITLAB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create an empty private repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitLab, private, GITLAB_ORG)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I git add-commit-push app workload to repo", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I should see workload is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have private visibility", func() {
			Expect(getGitRepoVisibility(GITLAB_ORG, tip.appRepoName, gitproviders.GitProviderGitLab)).Should(ContainSubstring("private"))
		})

		By("When I remove an app", func() {
			appRemoveOutput = runCommandAndReturnSessionOutput(WEGO_BIN_PATH + " app remove " + appName)
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(gbytes.Say("► Removing application from cluster and repository"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Pushing app changes to repository"))
		})

		By("And app should get deleted from the cluster", func() {
			_ = waitForAppRemoval(appName, THIRTY_SECOND_TIMEOUT)
		})
	})

	It("SmokeMy - Verify that gitops can deploy and remove a gitlab app that belongs in a subgroup", func() {
		var repoAbsolutePath string
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		var appRemoveOutput *gexec.Session

		addCommand := "app add . --auto-merge=true"

		var gitlabSubGroupPath = GITLAB_ORG + "/" + GITLAB_SUBGROUP

		defer deleteRepo(tip.appRepoName, gitlabSubGroupPath)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, gitlabSubGroupPath)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create an empty private repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitLab, private, gitlabSubGroupPath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I git add-commit-push app workload to repo", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I should see workload is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have private visibility", func() {
			Expect(getGitRepoVisibility(gitlabSubGroupPath, tip.appRepoName, gitproviders.GitProviderGitLab)).Should(ContainSubstring("private"))
		})

		By("When I remove an app", func() {
			appRemoveOutput = runCommandAndReturnSessionOutput(WEGO_BIN_PATH + " app remove " + appName)
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(gbytes.Say("► Removing application from cluster and repository"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Pushing app changes to repository"))
		})

		By("And app should get deleted from the cluster", func() {
			_ = waitForAppRemoval(appName, THIRTY_SECOND_TIMEOUT)
		})
	})

	It("Verify that gitops can deploy app when user specifies branch, namespace, url, deployment-type", func() {
		var repoAbsolutePath string
		private := true
		DEFAULT_SSH_KEY_PATH := "~/.ssh/id_rsa"
		tip := generateTestInputs()
		branchName := "test-branch-02"
		wegoNamespace := "my-space"
		appName := tip.appRepoName
		appRepoRemoteURL := "ssh://git@github.com/" + GITHUB_ORG + "/" + appName + ".git"

		addCommand := "app add --url=" + appRepoRemoteURL + " --branch=" + branchName + " --namespace=" + wegoNamespace + " --deployment-type=kustomize --app-config-url=NONE"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)
		defer uninstallWegoRuntime(wegoNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("And namespace: "+wegoNamespace+" doesn't exist", func() {
			uninstallWegoRuntime(wegoNamespace)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops under my namespace: "+wegoNamespace, func() {
			installAndVerifyWego(wegoNamespace)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I create a new branch", func() {
			createGitRepoBranch(repoAbsolutePath, branchName)
		})

		By("And I run gitops add command with specified branch, namespace, url, deployment-type", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, wegoNamespace)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, wegoNamespace)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And my app is deployed under specified branch name", func() {
			branchOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("kubectl get -n %s GitRepositories", wegoNamespace))
			Eventually(branchOutput).Should(ContainSubstring(appName))
			Eventually(branchOutput).Should(ContainSubstring(branchName))
		})

		By("And I should not see gitops components in the remote git repo", func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).ShouldNot(ContainSubstring("apps"))
			Expect(folderOutput).ShouldNot(ContainSubstring("targets"))
		})
	})

	It("Verify that gitops can deploy an app with specified config-url and app-config-url set to <url>", func() {
		var repoAbsolutePath string
		var configRepoRemoteURL string
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		appConfigRepoName := "wego-config-repo-" + RandString(8)
		appRepoRemoteURL := "ssh://git@github.com/" + GITHUB_ORG + "/" + tip.appRepoName + ".git"
		configRepoRemoteURL = "ssh://git@github.com/" + GITHUB_ORG + "/" + appConfigRepoName + ".git"

		addCommand := "app add --url=" + appRepoRemoteURL + " --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteRepo(appConfigRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
			deleteRepo(appConfigRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(appConfigRepoAbsPath, tip.appManifestFilePath)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command with --url and --app-config-url params", func() {
			runWegoAddCommand(repoAbsolutePath+"/../", addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})
	})

	It("Verify that gitops can deploy and remove a gitlab app with specified config-url and app-config-url set to <url>", func() {
		var repoAbsolutePath string
		var configRepoRemoteURL string
		private := true
		tip := generateTestInputs()
		var appRemoveOutput *gexec.Session
		appName := tip.appRepoName
		appConfigRepoName := "wego-config-repo-" + RandString(8)
		appRepoRemoteURL := "ssh://git@gitlab.com/" + GITLAB_ORG + "/" + tip.appRepoName + ".git"
		configRepoRemoteURL = "ssh://git@gitlab.com/" + GITLAB_ORG + "/" + appConfigRepoName + ".git"

		addCommand := "app add --url=" + appRepoRemoteURL + " --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITLAB_ORG)
		defer deleteRepo(appConfigRepoName, GITLAB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITLAB_ORG)
			deleteRepo(appConfigRepoName, GITLAB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitproviders.GitProviderGitLab, private, GITLAB_ORG)
			gitAddCommitPush(appConfigRepoAbsPath, tip.appManifestFilePath)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitLab, private, GITLAB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops add command with --url and --app-config-url params", func() {
			runWegoAddCommand(repoAbsolutePath+"/../", addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("When I remove an app", func() {
			appRemoveOutput = runCommandAndReturnSessionOutput(WEGO_BIN_PATH + " app remove " + appName)
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(gbytes.Say("► Removing application from cluster and repository"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Pushing app changes to repository"))
		})

		By("And app should get deleted from the cluster", func() {
			_ = waitForAppRemoval(appName, THIRTY_SECOND_TIMEOUT)
		})
	})

	It("Verify that gitops can deploy an app with specified config-url and app-config-url set to default", func() {
		var repoAbsolutePath string
		private := false
		tip := generateTestInputs()
		appName := tip.appRepoName
		appRepoRemoteURL := "ssh://git@github.com/" + GITHUB_ORG + "/" + tip.appRepoName + ".git"

		addCommand := "app add --url=" + appRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command with --url", func() {
			runWegoAddCommand(repoAbsolutePath+"/../", addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})
	})

	It("Verify that gitops can deploy an app when provided with relative path: 'path/to/repo/dir'", func() {
		var repoAbsolutePath string
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName

		addCommand := "app add " + tip.appRepoName + "/" + " --auto-merge=true"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip.workloadName, tip.workloadNamespace)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command from repo parent dir", func() {
			pathToRepoParentDir := repoAbsolutePath + "/../"
			runWegoAddCommand(pathToRepoParentDir, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And repos created have private visibility", func() {
			Expect(getGitRepoVisibility(GITHUB_ORG, tip.appRepoName, gitproviders.GitProviderGitHub)).Should(ContainSubstring("private"))
		})
	})

	It("Verify that gitops can deploy multiple workloads from a single app repo", func() {
		var repoAbsolutePath string
		tip1 := generateTestInputs()
		tip2 := generateTestInputs()
		appRepoName := "wego-test-app-" + RandString(8)
		appName := appRepoName

		addCommand := "app add . --name=" + appName + " --auto-merge=true"

		defer deleteRepo(appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
		defer deleteWorkload(tip2.workloadName, tip2.workloadNamespace)

		By("And application repos do not already exist", func() {
			deleteRepo(appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
			deleteWorkload(tip2.workloadName, tip2.workloadNamespace)
		})

		By("When I create an empty private repo for app1", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitproviders.GitProviderGitHub, true, GITHUB_ORG)
		})

		By("And I git add-commit-push for app with multiple workloads", func() {
			gitAddCommitPush(repoAbsolutePath, tip1.appManifestFilePath)
			gitAddCommitPush(repoAbsolutePath, tip2.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command for 1st app", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops add command linked the repo  to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I should see workload for app1 is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip1.workloadName, tip1.workloadNamespace)
			verifyWorkloadIsDeployed(tip2.workloadName, tip2.workloadNamespace)
		})
	})

	It("Verify that gitops can add multiple apps dir to the cluster using single repo for gitops config", func() {
		var repoAbsolutePath string
		var configRepoRemoteURL string
		private := true
		tip1 := generateTestInputs()
		tip2 := generateTestInputs()
		readmeFilePath := "./data/README.md"
		appRepoName1 := "wego-test-app-" + RandString(8)
		appRepoName2 := "wego-test-app-" + RandString(8)
		appConfigRepoName := "wego-config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@github.com/" + GITHUB_ORG + "/" + appConfigRepoName + ".git"
		appName1 := appRepoName1
		appName2 := appRepoName2

		addCommand := "app add . --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(appRepoName1, GITHUB_ORG)
		defer deleteRepo(appRepoName2, GITHUB_ORG)
		defer deleteRepo(appConfigRepoName, GITHUB_ORG)
		defer deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
		defer deleteWorkload(tip2.workloadName, tip2.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(appRepoName1, GITHUB_ORG)
			deleteRepo(appRepoName2, GITHUB_ORG)
			deleteRepo(appConfigRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
			deleteWorkload(tip2.workloadName, tip2.workloadNamespace)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(appConfigRepoAbsPath, readmeFilePath)
		})

		By("And I create a repo with my app1 workload and run the add the command on it", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName1, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip1.appManifestFilePath)
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I create a repo with my app2 workload and run the add the command on it", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName2, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip2.appManifestFilePath)
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workloads for app1 and app2 are deployed to the cluster", func() {
			verifyWegoAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyWegoAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip1.workloadName, tip1.workloadNamespace)
			verifyWorkloadIsDeployed(tip2.workloadName, tip2.workloadNamespace)
		})
	})

	It("Verify that gitops can add multiple apps dir to the cluster using single app and gitops config repo", func() {
		var repoAbsolutePath string
		private := true
		tip1 := generateTestInputs()
		tip2 := generateTestInputs()
		appRepoName := "wego-test-app-" + RandString(8)
		appName1 := "app1"
		appName2 := "app2"

		addCommand1 := "app add . --path=./" + appName1 + " --name=" + appName1 + " --auto-merge=true"
		addCommand2 := "app add . --path=./" + appName2 + " --name=" + appName2 + " --auto-merge=true"

		defer deleteRepo(appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
		defer deleteWorkload(tip2.workloadName, tip2.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
			deleteWorkload(tip2.workloadName, tip2.workloadNamespace)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I create a repo with my app1 and app2 workloads and run the add the command for each app", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			app1Path := createSubDir(appName1, repoAbsolutePath)
			app2Path := createSubDir(appName2, repoAbsolutePath)
			gitAddCommitPush(app1Path, tip1.appManifestFilePath)
			gitAddCommitPush(app2Path, tip2.appManifestFilePath)
			runWegoAddCommand(repoAbsolutePath, addCommand1, WEGO_DEFAULT_NAMESPACE)
			runWegoAddCommand(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workloads for app1 and app2 are deployed to the cluster", func() {
			verifyWegoAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyWegoAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip1.workloadName, tip1.workloadNamespace)
			verifyWorkloadIsDeployed(tip2.workloadName, tip2.workloadNamespace)
		})
	})

	It("Verify that gitops can deploy an app with app-config-url set to <url>", func() {
		var repoAbsolutePath string
		var configRepoRemoteURL string
		var listOutput string
		var appStatus1 string
		var appStatus2 string
		var appStatus3 string
		var commitList1 string
		var commitList2 string
		private := true
		readmeFilePath := "./data/README.md"
		tip := generateTestInputs()
		appFilesRepoName := tip.appRepoName
		appConfigRepoName := "wego-config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@github.com/" + GITHUB_ORG + "/" + appConfigRepoName + ".git"
		helmRepoURL := "https://charts.kube-ops.io"
		appName1 := appFilesRepoName
		workloadName1 := tip.workloadName
		workloadNamespace1 := tip.workloadNamespace
		appManifestFilePath1 := tip.appManifestFilePath
		appName2 := "my-helm-app"
		appManifestFilePath2 := "./data/helm-repo/hello-world"
		appName3 := "loki"
		workloadName3 := "loki-0"

		addCommand1 := "app add . --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"
		addCommand2 := "app add . --deployment-type=helm --path=./hello-world --name=" + appName2 + " --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"
		addCommand3 := "app add --url=" + helmRepoURL + " --chart=" + appName3 + " --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(appFilesRepoName, GITHUB_ORG)
		defer deleteRepo(appConfigRepoName, GITHUB_ORG)
		defer deleteWorkload(workloadName1, workloadNamespace1)
		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName3, EVENTUALLY_DEFAULT_TIMEOUT)

		By("And application repo does not already exist", func() {
			deleteRepo(appFilesRepoName, GITHUB_ORG)
			deleteRepo(appConfigRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(workloadName1, workloadNamespace1)
			deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName3, EVENTUALLY_DEFAULT_TIMEOUT)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(appConfigRepoAbsPath, readmeFilePath)
		})

		By("When I create a private repo with app1 workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appFilesRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath1)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops app add command for app1: "+appName1, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand1, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app1", func() {
			verifyWegoAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(workloadName1, workloadNamespace1)
		})

		By("When I add manifests for app2", func() {
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath2)
		})

		By("And I run gitops app add command for app2: "+appName2, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app2", func() {
			verifyWegoAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
			Expect(waitForResource("apps", appName2, WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
			Expect(waitForResource("configmaps", "helloworld-configmap", WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
		})

		By("When I run gitops app add command for app3: "+appName3, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand3, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app3", func() {
			verifyWegoHelmAddCommand(appName3, WEGO_DEFAULT_NAMESPACE)
			verifyHelmPodWorkloadIsDeployed(workloadName3, WEGO_DEFAULT_NAMESPACE)
		})

		By("When I check the app status for app1", func() {
			appStatus1, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app status " + appName1)
		})

		By("Then I should see the status for "+appName1, func() {
			Eventually(appStatus1).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus1).Should(ContainSubstring(`gitrepository/` + appName1))
			Eventually(appStatus1).Should(ContainSubstring(`kustomization/` + appName1))
		})

		By("When I check the app status for app2", func() {
			appStatus2, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app status " + appName2)
		})

		By("Then I should see the status for "+appName2, func() {
			Eventually(appStatus2).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus2).Should(ContainSubstring(`gitrepository/` + appName2))
			Eventually(appStatus2).Should(ContainSubstring(`helmrelease/` + appName2))
		})

		By("When I check the app status for app3", func() {
			appStatus3, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app status " + appName3)
		})

		By("Then I should see the status for "+appName3, func() {
			Eventually(appStatus3).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus3).Should(ContainSubstring(`helmrepository/` + appName3))
			Eventually(appStatus3).Should(ContainSubstring(`helmrelease/` + appName3))
		})

		By("When I check for apps list", func() {
			listOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app list")
		})

		By("Then I should see appNames for all apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName1))
			Eventually(listOutput).Should(ContainSubstring(appName2))
			Eventually(listOutput).Should(ContainSubstring(appName3))
		})

		By("And I should not see gitops components in app repo: "+appFilesRepoName, func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).ShouldNot(ContainSubstring("apps"))
			Expect(folderOutput).ShouldNot(ContainSubstring("targets"))
		})

		By("And I should see gitops components in config repo: "+appConfigRepoName, func() {
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && git clone %s && cd %s && ls -al", repoAbsolutePath, configRepoRemoteURL, appConfigRepoName))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).Should(ContainSubstring("apps"))
			Expect(folderOutput).Should(ContainSubstring("targets"))
		})

		By("When I check for list of commits for app1", func() {
			commitList1, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s app %s get commits", WEGO_BIN_PATH, appName1))
		})

		By("Then I should see the list of commits for app1", func() {
			Eventually(commitList1).Should(MatchRegexp(`COMMIT HASH\s*CREATED AT\s*AUTHOR\s*MESSAGE\s*URL`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
		})

		By("When I check for list of commits for app2", func() {
			commitList2, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s app %s get commits", WEGO_BIN_PATH, appName2))
		})

		By("Then I should see the list of commits for app2", func() {
			Eventually(commitList2).Should(MatchRegexp(`COMMIT HASH\s*CREATED AT\s*AUTHOR\s*MESSAGE\s*URL`))
			Eventually(commitList2).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
			Eventually(commitList2).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
		})
	})

	It("SmokeTest - Verify that gitops can deploy multiple apps one with private and other with public repo (e2e flow)", func() {
		var listOutput string
		var pauseOutput string
		var unpauseOutput string
		var appStatus1 *gexec.Session
		var appStatus2 *gexec.Session
		var appRemoveOutput *gexec.Session
		var repoAbsolutePath1 string
		var repoAbsolutePath2 string
		var appManifestFile1 string
		var commitList1 string
		var commitList2 string
		tip1 := generateTestInputs()
		tip2 := generateTestInputs()
		appName1 := tip1.appRepoName
		appName2 := tip2.appRepoName
		private := true
		public := false
		replicaSetValue := 3

		addCommand1 := "app add . --name=" + appName1 + " --auto-merge=true"
		addCommand2 := "app add . --name=" + appName2 + " --auto-merge=true"

		defer deleteRepo(tip1.appRepoName, GITHUB_ORG)
		defer deleteRepo(tip2.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
		defer deleteWorkload(tip2.workloadName, tip2.workloadNamespace)

		By("And application repos do not already exist", func() {
			deleteRepo(tip1.appRepoName, GITHUB_ORG)
			deleteRepo(tip2.appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(tip1.workloadName, tip1.workloadNamespace)
			deleteWorkload(tip2.workloadName, tip2.workloadNamespace)
		})

		By("When I create an empty private repo for app1", func() {
			repoAbsolutePath1 = initAndCreateEmptyRepo(tip1.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
		})

		By("When I create an empty public repo for app2", func() {
			repoAbsolutePath2 = initAndCreateEmptyRepo(tip2.appRepoName, gitproviders.GitProviderGitHub, public, GITHUB_ORG)
		})

		By("And I git add-commit-push for app1 with workload", func() {
			gitAddCommitPush(repoAbsolutePath1, tip1.appManifestFilePath)
		})

		By("And I git add-commit-push for app2 with workload", func() {
			gitAddCommitPush(repoAbsolutePath2, tip2.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops app add command for 1st app", func() {
			runWegoAddCommand(repoAbsolutePath1, addCommand1, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops app add command for 2nd app", func() {
			runWegoAddCommand(repoAbsolutePath2, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see gitops app add command linked the repo1 to the cluster", func() {
			verifyWegoAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I should see gitops app add command linked the repo2 to the cluster", func() {
			verifyWegoAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I should see workload for app1 is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip1.workloadName, tip1.workloadNamespace)
		})

		By("And I should see workload for app2 is deployed to the cluster", func() {
			verifyWorkloadIsDeployed(tip2.workloadName, tip2.workloadNamespace)
		})

		By("And repos created have proper visibility", func() {
			Eventually(getGitRepoVisibility(GITHUB_ORG, tip1.appRepoName, gitproviders.GitProviderGitHub)).Should(ContainSubstring("private"))
			Eventually(getGitRepoVisibility(GITHUB_ORG, tip2.appRepoName, gitproviders.GitProviderGitHub)).Should(ContainSubstring("public"))
		})

		By("When I check the app status for "+appName1, func() {
			appStatus1 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s app status %s", WEGO_BIN_PATH, appName1))
		})

		By("Then I should see the status for "+appName1, func() {
			Eventually(appStatus1).Should(gbytes.Say(`Last successful reconciliation:`))
			Eventually(appStatus1).Should(gbytes.Say(`gitrepository/` + appName1))
			Eventually(appStatus1).Should(gbytes.Say(`kustomization/` + appName1))
		})

		By("When I check the app status for "+appName2, func() {
			appStatus2 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s app status %s", WEGO_BIN_PATH, appName2))
		})

		By("Then I should see the status for "+appName2, func() {
			Eventually(appStatus2).Should(gbytes.Say(`Last successful reconciliation:`))
			Eventually(appStatus2).Should(gbytes.Say(`gitrepository/` + appName2))
			Eventually(appStatus2).Should(gbytes.Say(`kustomization/` + appName2))
		})

		By("When I check for apps list", func() {
			listOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app list")
		})

		By("Then I should see appNames for both apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName1))
			Eventually(listOutput).Should(ContainSubstring(appName2))
		})

		By("When I pause an app: "+appName1, func() {
			pauseOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app pause " + appName1)
		})

		By("Then I should see pause message", func() {
			Expect(pauseOutput).To(ContainSubstring("gitops automation paused for " + appName1))
		})

		By("When I check app status for paused app", func() {
			appStatus1 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s app status %s", WEGO_BIN_PATH, appName1))
		})

		By("Then I should see pause status as suspended=true", func() {
			Eventually(appStatus1).Should(gbytes.Say(`kustomization/` + appName1 + `\s*True\s*.*True`))
		})

		By("And changes to the app files should not be synchronized", func() {
			appManifestFile1, _ = runCommandAndReturnStringOutput("cd " + repoAbsolutePath1 + " && ls | grep yaml")
			createAppReplicas(repoAbsolutePath1, appManifestFile1, replicaSetValue, tip1.workloadName)
			gitUpdateCommitPush(repoAbsolutePath1)
			_ = waitForReplicaCreation(tip1.workloadNamespace, replicaSetValue, EVENTUALLY_DEFAULT_TIMEOUT)
			_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl wait --for=condition=Ready --timeout=100s -n %s --all pods", tip1.workloadNamespace))
		})

		By("And number of app replicas should remain same", func() {
			replicaOutput, _ := runCommandAndReturnStringOutput("kubectl get pods -n " + tip1.workloadNamespace + " --field-selector=status.phase=Running --no-headers=true | wc -l")
			Expect(replicaOutput).To(ContainSubstring("1"))
		})

		By("When I re-run app pause command", func() {
			pauseOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app pause " + appName1)
		})

		By("Then I should see a console message without any errors", func() {
			Expect(pauseOutput).To(ContainSubstring("app " + appName1 + " is already paused"))
		})

		By("When I unpause an app: "+appName1, func() {
			unpauseOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app unpause " + appName1)
		})

		By("Then I should see unpause message", func() {
			Expect(unpauseOutput).To(ContainSubstring("gitops automation unpaused for " + appName1))
		})

		By("And I should see app replicas created in the cluster", func() {
			_ = waitForReplicaCreation(tip1.workloadNamespace, replicaSetValue, EVENTUALLY_DEFAULT_TIMEOUT)
			_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("kubectl wait --for=condition=Ready --timeout=100s -n %s --all pods", tip1.workloadNamespace))
			replicaOutput, _ := runCommandAndReturnStringOutput("kubectl get pods -n " + tip1.workloadNamespace + " --field-selector=status.phase=Running --no-headers=true | wc -l")
			Expect(replicaOutput).To(ContainSubstring(strconv.Itoa(replicaSetValue)))
		})

		By("When I re-run app unpause command", func() {
			unpauseOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app unpause " + appName1)
		})

		By("Then I should see unpause message without any errors", func() {
			Expect(unpauseOutput).To(ContainSubstring("app " + appName1 + " is already reconciling"))
		})

		By("When I check app status for unpaused app", func() {
			appStatus1 = runCommandAndReturnSessionOutput(fmt.Sprintf("%s app status %s", WEGO_BIN_PATH, appName1))
		})

		By("Then I should see pause status as suspended=false", func() {
			Eventually(appStatus1).Should(gbytes.Say(`kustomization/` + appName1 + `\s*True\s*.*False`))
		})

		By("When I check for list of commits for app2", func() {
			commitList2, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s app %s get commits", WEGO_BIN_PATH, appName2))
		})

		By("Then I should see the list of commits for app2", func() {
			Eventually(commitList2).Should(MatchRegexp(`COMMIT HASH\s*CREATED AT\s*AUTHOR\s*MESSAGE\s*URL`))
			Eventually(commitList2).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z\s*Weave Gitops\s*Add App manifests`))
			Eventually(commitList2).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
			Eventually(commitList2).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
		})

		By("When I remove an app", func() {
			appRemoveOutput = runCommandAndReturnSessionOutput(WEGO_BIN_PATH + " app remove " + appName2)
		})

		By("Then I should see app removing message", func() {
			Eventually(appRemoveOutput).Should(gbytes.Say("► Removing application from cluster and repository"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Committing and pushing gitops updates for application"))
			Eventually(appRemoveOutput).Should(gbytes.Say("► Pushing app changes to repository"))
		})

		By("And app should get deleted from the cluster", func() {
			_ = waitForAppRemoval(appName2, THIRTY_SECOND_TIMEOUT)
		})

		By("When I check for list of commits for app1", func() {
			commitList1, _ = runCommandAndReturnStringOutput(fmt.Sprintf("%s app %s get commits", WEGO_BIN_PATH, appName1))
		})

		By("Then I should see the list of commits for app1", func() {
			Eventually(commitList1).Should(MatchRegexp(`COMMIT HASH\s*CREATED AT\s*AUTHOR\s*MESSAGE\s*URL`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z\s*Weave Gitops\s*Add App manifests`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
			Eventually(commitList1).Should(MatchRegexp(`[\w]{7}\s*202\d-[0,1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-5][0-9]Z`))
		})

		By("When I check for list of commits for a deleted app", func() {
			_, commitList2 = runCommandAndReturnStringOutput(fmt.Sprintf("%s app %s get commits", WEGO_BIN_PATH, appName2))
		})

		By("Then I should see the list of commits for app2", func() {
			Eventually(commitList2).Should(ContainSubstring(`Error:`))
			Eventually(commitList2).Should(MatchRegexp(`\"` + appName2 + `\" not found`))
		})
	})

	It("SmokeTest - Verify that gitops can deploy a helm app from a git repo with app-config-url set to NONE", func() {
		var repoAbsolutePath string
		var reAddOutput string
		var removeOutput *gexec.Session
		private := true
		appManifestFilePath := "./data/helm-repo/hello-world"
		appName := "my-helm-app"
		appRepoName := "wego-test-app-" + RandString(8)
		badAppName := "foo"

		addCommand := "app add . --deployment-type=helm --path=./hello-world --name=" + appName + " --app-config-url=NONE"

		defer deleteRepo(appRepoName, GITHUB_ORG)

		By("Application and config repo does not already exist", func() {
			deleteRepo(appRepoName, GITHUB_ORG)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			Expect(waitForResource("apps", appName, WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
			Expect(waitForResource("configmaps", "helloworld-configmap", WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
		})

		By("And I should not see gitops components in the remote git repo", func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).ShouldNot(ContainSubstring("apps"))
			Expect(folderOutput).ShouldNot(ContainSubstring("targets"))
		})

		By("When I rerun gitops install", func() {
			_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("%s install", WEGO_BIN_PATH))
		})

		By("Then I should not see any errors", func() {
			VerifyControllersInCluster(WEGO_DEFAULT_NAMESPACE)
		})

		By("When I rerun gitops app add command", func() {
			_, reAddOutput = runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && %s %s", repoAbsolutePath, WEGO_BIN_PATH, addCommand))
		})

		By("Then I should see an error", func() {
			Eventually(reAddOutput).Should(ContainSubstring("Error: failed to add the app " + appName + ": unable to create resource, resource already exists in cluster"))
		})

		By("And app status should remain same", func() {
			out := runCommandAndReturnSessionOutput(WEGO_BIN_PATH + " app status " + appName)
			Eventually(out).Should(gbytes.Say(`helmrelease/` + appName + `\s*True\s*.*False`))
		})

		By("When I run gitops app remove", func() {
			_ = runCommandPassThrough([]string{}, "sh", "-c", fmt.Sprintf("%s app remove %s", WEGO_BIN_PATH, appName))
		})

		By("Then I should see app removed from the cluster", func() {
			_ = waitForAppRemoval(appName, THIRTY_SECOND_TIMEOUT)
		})

		By("When I run gitops app remove for a non-existent app", func() {
			removeOutput = runCommandAndReturnSessionOutput(WEGO_BIN_PATH + " app remove " + badAppName)
		})

		By("Then I should get an error", func() {
			Eventually(removeOutput.Err).Should(gbytes.Say(`Error: failed to create app service: error getting git clients: could not retrieve application "` + badAppName + `": could not get application: apps.wego.weave.works "` + badAppName + `" not found`))
		})
	})

	It("Verify that gitops can deploy a helm app from a git repo with app-config-url set to default", func() {
		var repoAbsolutePath string
		public := false
		appName := "my-helm-app"
		appManifestFilePath := "./data/helm-repo/hello-world"
		appRepoName := "wego-test-app-" + RandString(8)

		addCommand := "app add . --deployment-type=helm --path=./hello-world --name=" + appName + " --auto-merge=true"

		defer deleteRepo(appRepoName, GITHUB_ORG)

		By("And application repo does not already exist", func() {
			deleteRepo(appRepoName, GITHUB_ORG)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitproviders.GitProviderGitHub, public, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			Expect(waitForResource("apps", appName, WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
			Expect(waitForResource("configmaps", "helloworld-configmap", WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
		})

		By("And repo created has public visibility", func() {
			Eventually(getGitRepoVisibility(GITHUB_ORG, appRepoName, gitproviders.GitProviderGitHub)).Should(ContainSubstring("public"))
		})
	})

	It("Verify that gitops can deploy a helm app from a git repo with app-config-url set to <url>", func() {
		var repoAbsolutePath string
		var configRepoAbsolutePath string
		private := true
		appManifestFilePath := "./data/helm-repo/hello-world"
		configRepoFiles := "./data/config-repo"
		appName := "my-helm-app"
		appRepoName := "wego-test-app-" + RandString(8)
		configRepoName := "wego-test-config-repo-" + RandString(8)
		configRepoUrl := fmt.Sprintf("ssh://git@github.com/%s/%s.git", os.Getenv("GITHUB_ORG"), configRepoName)

		addCommand := fmt.Sprintf("app add . --app-config-url=%s --deployment-type=helm --path=./hello-world --name=%s --auto-merge=true", configRepoUrl, appName)

		defer deleteRepo(appRepoName, GITHUB_ORG)
		defer deleteRepo(configRepoName, GITHUB_ORG)

		By("Application and config repo does not already exist", func() {
			deleteRepo(appRepoName, GITHUB_ORG)
			deleteRepo(configRepoName, GITHUB_ORG)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("When I create a private repo for my config files", func() {
			configRepoAbsolutePath = initAndCreateEmptyRepo(configRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(configRepoAbsolutePath, configRepoFiles)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("There is no .wego folder in the app repo", func() {
			_, err := os.Stat(repoAbsolutePath + "/.wego")
			Expect(os.IsNotExist(err)).To(Equal(true))
		})

		By("The manifests are present in the config repo", func() {
			pullBranch(configRepoAbsolutePath, "main")

			_, err := os.Stat(fmt.Sprintf("%s/apps/%s/app.yaml", configRepoAbsolutePath, appName))
			Expect(err).ShouldNot(HaveOccurred())

			_, err = os.Stat(fmt.Sprintf("%s/targets/%s/%s/%s-gitops-source.yaml", configRepoAbsolutePath, clusterName, appName, appName))
			Expect(err).ShouldNot(HaveOccurred())

			_, err = os.Stat(fmt.Sprintf("%s/targets/%s/%s/%s-gitops-deploy.yaml", configRepoAbsolutePath, clusterName, appName, appName))
			Expect(err).ShouldNot(HaveOccurred())
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			Expect(waitForResource("apps", appName, WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
			Expect(waitForResource("configmaps", "helloworld-configmap", WEGO_DEFAULT_NAMESPACE, INSTALL_PODS_READY_TIMEOUT)).To(Succeed())
		})

	})

	It("Verify that gitops can deploy multiple helm apps from a helm repo with app-config-url set to <url>", func() {
		var repoAbsolutePath string
		var listOutput string
		var appStatus1 string
		var appStatus2 string
		private := true
		appName1 := "rabbitmq"
		appName2 := "zookeeper"
		workloadName1 := "rabbitmq-0"
		workloadName2 := "test-space-zookeeper-0"
		workloadNamespace2 := "test-space"
		readmeFilePath := "./data/README.md"
		appRepoName := "wego-test-app-" + RandString(8)
		appRepoRemoteURL := "ssh://git@github.com/" + GITHUB_ORG + "/" + appRepoName + ".git"
		helmRepoURL := "https://charts.bitnami.com/bitnami"

		addCommand1 := "app add --url=" + helmRepoURL + " --chart=" + appName1 + " --app-config-url=" + appRepoRemoteURL + " --auto-merge=true"
		addCommand2 := "app add --url=" + helmRepoURL + " --chart=" + appName2 + " --app-config-url=" + appRepoRemoteURL + " --auto-merge=true --helm-release-target-namespace=" + workloadNamespace2

		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName1, EVENTUALLY_DEFAULT_TIMEOUT)
		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName2, EVENTUALLY_DEFAULT_TIMEOUT)
		defer deleteRepo(appRepoName, GITHUB_ORG)
		defer deleteNamespace(workloadNamespace2)

		By("And application repo does not already exist", func() {
			deleteRepo(appRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName1, EVENTUALLY_DEFAULT_TIMEOUT)
			deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName2, EVENTUALLY_DEFAULT_TIMEOUT)
		})

		By("When I create a private git repo", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, readmeFilePath)
		})

		By("And I install gitops under my namespace: "+WEGO_DEFAULT_NAMESPACE, func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I create a namespace for helm-app", func() {
			out, _ := runCommandAndReturnStringOutput("kubectl create ns " + workloadNamespace2)
			Eventually(out).Should(ContainSubstring("namespace/" + workloadNamespace2 + " created"))
		})

		By("And I run gitops app add command for 1st app", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand1, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I run gitops app add command for 2nd app", func() {
			runWegoAddCommand(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see workload1 deployed to the cluster", func() {
			verifyWegoHelmAddCommand(appName1, WEGO_DEFAULT_NAMESPACE)
			verifyHelmPodWorkloadIsDeployed(workloadName1, WEGO_DEFAULT_NAMESPACE)
		})

		By("And I should see workload2 deployed to the cluster", func() {
			verifyWegoHelmAddCommand(appName2, WEGO_DEFAULT_NAMESPACE)
			verifyHelmPodWorkloadIsDeployed(workloadName2, workloadNamespace2)
		})

		By("And I should see gitops components in the remote git repo", func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).Should(ContainSubstring("apps"))
			Expect(folderOutput).Should(ContainSubstring("targets"))
		})

		By("When I check for apps list", func() {
			listOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app list")
		})

		By("Then I should see appNames for both apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName1))
			Eventually(listOutput).Should(ContainSubstring(appName2))
		})

		By("When I check the app status for "+appName1, func() {
			appStatus1, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app status " + appName1)
		})

		By("Then I should see the status for app1", func() {
			Eventually(appStatus1).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus1).Should(ContainSubstring(`helmrepository/` + appName1))
			Eventually(appStatus1).Should(ContainSubstring(`helmrelease/` + appName1))
		})

		By("When I check the app status for "+appName2, func() {
			appStatus2, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app status " + appName2)
		})

		By("Then I should see the status for app2", func() {
			Eventually(appStatus2).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus2).Should(ContainSubstring(`helmrepository/` + appName2))
			Eventually(appStatus2).Should(ContainSubstring(`helmrelease/` + appName2))
		})
	})

	It("Verify that gitops can deploy a helm app from a helm repo with app-config-url set to NONE", func() {
		appName := "loki"
		workloadName := "loki-0"
		helmRepoURL := "https://charts.kube-ops.io"

		addCommand := "app add --url=" + helmRepoURL + " --chart=" + appName + " --app-config-url=NONE"

		defer deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName, EVENTUALLY_DEFAULT_TIMEOUT)

		By("And application workload is not already deployed to cluster", func() {
			deletePersistingHelmApp(WEGO_DEFAULT_NAMESPACE, workloadName, EVENTUALLY_DEFAULT_TIMEOUT)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command", func() {
			runWegoAddCommand(".", addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoHelmAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyHelmPodWorkloadIsDeployed(workloadName, WEGO_DEFAULT_NAMESPACE)
		})
	})

	It("Verify that a PR is raised against a user repo when skipping auto-merge", func() {
		var repoAbsolutePath string
		tip := generateTestInputs()
		appName := tip.appRepoName
		prLink := ""

		addCommand := "app add . --name=" + appName + " --auto-merge=false"

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("When I create an empty private repo for app", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, true, GITHUB_ORG)
		})

		By("And I git add-commit-push app manifest", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("When I run gitops app add command for app", func() {
			output, _ := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
			re := regexp.MustCompile(`(http|ftp|https):\/\/([\w\-_]+(?:(?:\.[\w\-_]+)+))([\w\-\.,@?^=%&amp;:/~\+#]*[\w\-\@?^=%&amp;/~\+#])?`)
			prLink = re.FindAllString(output, -1)[0]
		})

		By("Then I should see a PR created in user repo", func() {
			verifyPRCreated(repoAbsolutePath, appName)
		})

		By("When I merge the created PR", func() {
			mergePR(repoAbsolutePath, prLink)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})
	})

	It("Verify that a PR can be raised against an external repo with app-config-url set to <url>", func() {
		var repoAbsolutePath string
		var configRepoRemoteURL string
		var appConfigRepoAbsPath string
		prLink := ""
		private := true
		tip := generateTestInputs()
		appName := tip.appRepoName
		appConfigRepoName := "wego-config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@github.com/" + GITHUB_ORG + "/" + appConfigRepoName + ".git"

		addCommand := "app add . --app-config-url=" + configRepoRemoteURL

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteRepo(appConfigRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
			deleteRepo(appConfigRepoName, GITHUB_ORG)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath = initAndCreateEmptyRepo(appConfigRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(appConfigRepoAbsPath, tip.appManifestFilePath)
		})

		By("When I create a private repo with my app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops add command with --app-config-url param", func() {
			output, _ := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
			re := regexp.MustCompile(`(http|https):\/\/([\w\-_]+(?:(?:\.[\w\-_]+)+))([\w\-\.,@?^=%&amp;:/~\+#]*[\w\-\@?^=%&amp;/~\+#])?`)
			prLink = re.FindAllString(output, 1)[0]
		})

		By("Then I should see a PR created for external repo", func() {
			verifyPRCreated(appConfigRepoAbsPath, appName)
		})

		By("When I merge the created PR", func() {
			mergePR(appConfigRepoAbsPath, prLink)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})
	})

	It("Verify that a PR fails when raised against the same app-repo with different branch and app", func() {
		var repoAbsolutePath string
		tip := generateTestInputs()
		tip2 := generateTestInputs()
		appName := tip.appRepoName
		appName2 := tip2.appRepoName
		prLink := "https://github.com/" + GITHUB_ORG + "/" + tip.appRepoName + "/pull/1"

		addCommand := "app add . --name=" + appName
		addCommand2 := "app add . --name=" + appName2

		defer deleteRepo(tip.appRepoName, GITHUB_ORG)
		defer deleteWorkload(tip.workloadName, tip.workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(tip.appRepoName, GITHUB_ORG)
		})

		By("When I create an empty private repo for app", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(tip.appRepoName, gitproviders.GitProviderGitHub, true, GITHUB_ORG)
		})

		By("And I git add-commit-push for app with workload", func() {
			gitAddCommitPush(repoAbsolutePath, tip.appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run app add command for "+appName, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see a PR created for "+appName, func() {
			verifyPRCreated(repoAbsolutePath, appName)
		})

		By("And I should fail to create a PR with the same app repo consecutively", func() {
			_, addCommandErr := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
			Expect(addCommandErr).Should(ContainSubstring("422 Reference already exists"))
		})

		By("When I merge the previous PR", func() {
			mergePR(repoAbsolutePath, prLink)
		})

		By("Then I should see my workload deployed to the cluster", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(tip.workloadName, tip.workloadNamespace)
		})

		By("And I should fail to create another PR with the same app", func() {
			_, addCommandErr := runWegoAddCommandWithOutput(repoAbsolutePath, addCommand2, WEGO_DEFAULT_NAMESPACE)
			Expect(addCommandErr).Should(ContainSubstring("unable to create resource, resource already exists in cluster"))
		})
	})
})

var _ = Describe("Weave GitOps Add Tests With Long Cluster Name", func() {
	deleteWegoRuntime := false
	if os.Getenv("DELETE_WEGO_RUNTIME_ON_EACH_TEST") == "true" {
		deleteWegoRuntime = true
	}

	var _ = BeforeEach(func() {
		By("Given I have a brand new cluster with a long cluster name", func() {
			var err error

			clusterName = "kind-123456789012345678901234567890"
			_, err = ResetOrCreateClusterWithName(WEGO_DEFAULT_NAMESPACE, deleteWegoRuntime, clusterName)
			Expect(err).ShouldNot(HaveOccurred())
		})

		By("And I have a gitops binary installed on my local machine", func() {
			Expect(FileExists(WEGO_BIN_PATH)).To(BeTrue())
		})
	})

	It("SmokeTest - Verify that gitops can deploy an app with app-config-url set to <url>", func() {
		var repoAbsolutePath string
		var configRepoRemoteURL string
		var listOutput string
		var appStatus string
		private := true
		readmeFilePath := "./data/README.md"
		tip := generateTestInputs()
		appFilesRepoName := tip.appRepoName + "123456789012345678901234567890"
		appConfigRepoName := "wego-config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@github.com/" + GITHUB_ORG + "/" + appConfigRepoName + ".git"
		appName := appFilesRepoName
		workloadName := tip.workloadName
		workloadNamespace := tip.workloadNamespace
		appManifestFilePath := tip.appManifestFilePath

		addCommand := "app add . --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(appFilesRepoName, GITHUB_ORG)
		defer deleteRepo(appConfigRepoName, GITHUB_ORG)
		defer deleteWorkload(workloadName, workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(appFilesRepoName, GITHUB_ORG)
			deleteRepo(appConfigRepoName, GITHUB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(workloadName, workloadNamespace)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(appConfigRepoAbsPath, readmeFilePath)
		})

		By("When I create a private repo with app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appFilesRepoName, gitproviders.GitProviderGitHub, private, GITHUB_ORG)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops app add command for app: "+appName, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(workloadName, workloadNamespace)
		})

		By("When I check the app status for app", func() {
			appStatus, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app status " + appName)
		})

		By("Then I should see the status for "+appName, func() {
			Eventually(appStatus).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus).Should(ContainSubstring(`gitrepository/` + appName))
			Eventually(appStatus).Should(ContainSubstring(`kustomization/` + appName))
		})

		By("When I check for apps list", func() {
			listOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app list")
		})

		By("Then I should see appNames for all apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName))
		})

		By("And I should not see gitops components in app repo: "+appFilesRepoName, func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).ShouldNot(ContainSubstring("apps"))
			Expect(folderOutput).ShouldNot(ContainSubstring("targets"))
		})

		By("And I should see gitops components in config repo: "+appConfigRepoName, func() {
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && git clone %s && cd %s && ls -al", repoAbsolutePath, configRepoRemoteURL, appConfigRepoName))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).Should(ContainSubstring("apps"))
			Expect(folderOutput).Should(ContainSubstring("targets"))
		})
	})

	It("SmokeTest - Verify that gitops can deploy an app with app-config-url set to a gitlab <url>", func() {
		var repoAbsolutePath string
		var configRepoRemoteURL string
		var listOutput string
		var appStatus string
		private := true
		readmeFilePath := "./data/README.md"
		tip := generateTestInputs()
		appFilesRepoName := tip.appRepoName + "123456789012345678901234567890"
		appConfigRepoName := "wego-config-repo-" + RandString(8)
		configRepoRemoteURL = "ssh://git@gitlab.com/" + GITLAB_ORG + "/" + appConfigRepoName + ".git"
		appName := appFilesRepoName
		workloadName := tip.workloadName
		workloadNamespace := tip.workloadNamespace
		appManifestFilePath := tip.appManifestFilePath

		addCommand := "app add . --app-config-url=" + configRepoRemoteURL + " --auto-merge=true"

		defer deleteRepo(appFilesRepoName, GITLAB_ORG)
		defer deleteRepo(appConfigRepoName, GITLAB_ORG)
		defer deleteWorkload(workloadName, workloadNamespace)

		By("And application repo does not already exist", func() {
			deleteRepo(appFilesRepoName, GITLAB_ORG)
			deleteRepo(appConfigRepoName, GITLAB_ORG)
		})

		By("And application workload is not already deployed to cluster", func() {
			deleteWorkload(workloadName, workloadNamespace)
		})

		By("When I create a private repo for gitops app config", func() {
			appConfigRepoAbsPath := initAndCreateEmptyRepo(appConfigRepoName, gitproviders.GitProviderGitLab, private, GITLAB_ORG)
			gitAddCommitPush(appConfigRepoAbsPath, readmeFilePath)
		})

		By("When I create a private repo with app workload", func() {
			repoAbsolutePath = initAndCreateEmptyRepo(appFilesRepoName, gitproviders.GitProviderGitLab, private, GITLAB_ORG)
			gitAddCommitPush(repoAbsolutePath, appManifestFilePath)
		})

		By("And I install gitops to my active cluster", func() {
			installAndVerifyWego(WEGO_DEFAULT_NAMESPACE)
		})

		By("And I have my default ssh key on path "+DEFAULT_SSH_KEY_PATH, func() {
			setupSSHKey(DEFAULT_SSH_KEY_PATH)
		})

		By("And I run gitops app add command for app: "+appName, func() {
			runWegoAddCommand(repoAbsolutePath, addCommand, WEGO_DEFAULT_NAMESPACE)
		})

		By("Then I should see my workload deployed for app", func() {
			verifyWegoAddCommand(appName, WEGO_DEFAULT_NAMESPACE)
			verifyWorkloadIsDeployed(workloadName, workloadNamespace)
		})

		By("When I check the app status for app", func() {
			appStatus, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app status " + appName)
		})

		By("Then I should see the status for "+appName, func() {
			Eventually(appStatus).Should(ContainSubstring(`Last successful reconciliation:`))
			Eventually(appStatus).Should(ContainSubstring(`gitrepository/` + appName))
			Eventually(appStatus).Should(ContainSubstring(`kustomization/` + appName))
		})

		By("When I check for apps list", func() {
			listOutput, _ = runCommandAndReturnStringOutput(WEGO_BIN_PATH + " app list")
		})

		By("Then I should see appNames for all apps listed", func() {
			Eventually(listOutput).Should(ContainSubstring(appName))
		})

		By("And I should not see gitops components in app repo: "+appFilesRepoName, func() {
			pullGitRepo(repoAbsolutePath)
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && ls -al", repoAbsolutePath))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).ShouldNot(ContainSubstring("apps"))
			Expect(folderOutput).ShouldNot(ContainSubstring("targets"))
		})

		By("And I should see gitops components in config repo: "+appConfigRepoName, func() {
			folderOutput, _ := runCommandAndReturnStringOutput(fmt.Sprintf("cd %s && git clone %s && cd %s && ls -al", repoAbsolutePath, configRepoRemoteURL, appConfigRepoName))
			Expect(folderOutput).ShouldNot(ContainSubstring(".wego"))
			Expect(folderOutput).Should(ContainSubstring("apps"))
			Expect(folderOutput).Should(ContainSubstring("targets"))
		})
	})
})
