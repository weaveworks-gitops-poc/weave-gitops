package gitproviders

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/weaveworks/weave-gitops/pkg/utils"

	"github.com/fluxcd/go-git-providers/gitprovider"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate

type ProviderAccountType string

const (
	AccountTypeUser ProviderAccountType = "user"
	AccountTypeOrg  ProviderAccountType = "organization"
	deployKeyName                       = "wego-deploy-key"

	defaultTimeout = time.Second * 30
)

// GitProvider Handler
//counterfeiter:generate . GitProvider
type GitProvider interface {
	CreateRepository(name string, owner string, private bool) error
	RepositoryExists(name string, owner string) (bool, error)
	DeployKeyExists(owner, repoName string) (bool, error)
	GetRepoInfo(accountType ProviderAccountType, owner string, repoName string) (*gitprovider.RepositoryInfo, error)
	GetRepoInfoFromUrl(url string) (*gitprovider.RepositoryInfo, error)
	GetDefaultBranch(url string) (string, error)
	GetRepoVisibility(url string) (*gitprovider.RepositoryVisibility, error)
	UploadDeployKey(owner, repoName string, deployKey []byte) error
	CreatePullRequestToUserRepo(userRepRef gitprovider.UserRepositoryRef, targetBranch string, newBranch string, files []gitprovider.CommitFile, commitMessage string, prTitle string, prDescription string) (gitprovider.PullRequest, error)
	CreatePullRequestToOrgRepo(orgRepRef gitprovider.OrgRepositoryRef, targetBranch string, newBranch string, files []gitprovider.CommitFile, commitMessage string, prTitle string, prDescription string) (gitprovider.PullRequest, error)
	GetCommitsFromUserRepo(userRepRef gitprovider.UserRepositoryRef, targetBranch string, pageSize int, pageToken int) ([]gitprovider.Commit, error)
	GetCommitsFromOrgRepo(orgRepRef gitprovider.OrgRepositoryRef, targetBranch string, pageSize int, pageToken int) ([]gitprovider.Commit, error)
	GetAccountType(owner string) (ProviderAccountType, error)
	GetProviderDomain() string
}

type defaultGitProvider struct {
	domain   string
	provider gitprovider.Client
}

func New(config Config) (GitProvider, error) {
	provider, domain, err := buildGitProvider(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build git provider: %w", err)
	}

	return defaultGitProvider{
		domain:   domain,
		provider: provider,
	}, nil
}

func (p defaultGitProvider) RepositoryExists(name string, owner string) (bool, error) {
	ownerType, err := p.GetAccountType(owner)
	if err != nil {
		return false, err
	}

	ctx := context.Background()

	if ownerType == AccountTypeOrg {
		orgRef := gitprovider.OrgRepositoryRef{
			OrganizationRef: gitprovider.OrganizationRef{Domain: p.domain, Organization: owner},
			RepositoryName:  name,
		}
		if _, err := p.provider.OrgRepositories().Get(ctx, orgRef); err != nil {
			return false, err
		}

		return true, nil
	}

	userRepoRef := gitprovider.UserRepositoryRef{
		UserRef:        gitprovider.UserRef{Domain: p.domain, UserLogin: owner},
		RepositoryName: name,
	}
	if _, err := p.provider.UserRepositories().Get(ctx, userRepoRef); err != nil {
		return false, err
	}

	return true, nil
}

func (p defaultGitProvider) CreateRepository(name string, owner string, private bool) error {
	visibility := gitprovider.RepositoryVisibilityPrivate
	if !private {
		visibility = gitprovider.RepositoryVisibilityPublic
	}

	repoInfo := NewRepositoryInfo("Weave Gitops repo", visibility)

	repoCreateOpts := &gitprovider.RepositoryCreateOptions{
		AutoInit:        gitprovider.BoolVar(true),
		LicenseTemplate: gitprovider.LicenseTemplateVar(gitprovider.LicenseTemplateApache2),
	}

	ownerType, err := p.GetAccountType(owner)
	if err != nil {
		return err
	}

	if ownerType == AccountTypeOrg {
		orgRef := NewOrgRepositoryRef(p.domain, owner, name)
		if err = p.CreateOrgRepository(orgRef, repoInfo, repoCreateOpts); err != nil {
			return err
		}
	} else {
		userRef := NewUserRepositoryRef(p.domain, owner, name)
		if err = p.CreateUserRepository(userRef, repoInfo, repoCreateOpts); err != nil {
			return err
		}
	}

	return nil
}

func (p defaultGitProvider) DeployKeyExists(owner, repoName string) (bool, error) {
	ownerType, err := p.GetAccountType(owner)
	if err != nil {
		return false, err
	}

	ctx := context.Background()
	defer ctx.Done()

	switch ownerType {
	case AccountTypeOrg:
		orgRef := NewOrgRepositoryRef(p.domain, owner, repoName)
		orgRepo, err := p.provider.OrgRepositories().Get(ctx, orgRef)

		if err != nil {
			return false, fmt.Errorf("error getting org repo reference for owner %s, repo %s, %s ", owner, repoName, err)
		}

		_, err = orgRepo.DeployKeys().Get(ctx, deployKeyName)
		if err != nil && !strings.Contains(err.Error(), "key is already in use") {
			if errors.Is(err, gitprovider.ErrNotFound) {
				return false, nil
			} else {
				return false, fmt.Errorf("error getting deploy key %s for repo %s. %s", deployKeyName, repoName, err)
			}
		} else {
			return true, nil
		}

	case AccountTypeUser:
		userRef := NewUserRepositoryRef(p.domain, owner, repoName)
		userRepo, err := p.provider.UserRepositories().Get(ctx, userRef)

		if err != nil {
			return false, fmt.Errorf("error getting user repo reference for owner %s, repo %s, %s ", owner, repoName, err)
		}

		_, err = userRepo.DeployKeys().Get(ctx, deployKeyName)
		if err != nil && !strings.Contains(err.Error(), "key is already in use") {
			if errors.Is(err, gitprovider.ErrNotFound) {
				return false, nil
			} else {
				return false, fmt.Errorf("error getting deploy key %s for repo %s. %s", deployKeyName, repoName, err)
			}
		} else {
			return true, nil
		}
	default:
		return false, fmt.Errorf("account type not supported %s", ownerType)
	}
}

func (p defaultGitProvider) UploadDeployKey(owner, repoName string, deployKey []byte) error {
	deployKeyInfo := gitprovider.DeployKeyInfo{
		Name:     deployKeyName,
		Key:      deployKey,
		ReadOnly: gitprovider.BoolVar(false),
	}

	ownerType, err := p.GetAccountType(owner)
	if err != nil {
		return err
	}

	ctx := context.Background()
	defer ctx.Done()

	switch ownerType {
	case AccountTypeOrg:
		orgRef := NewOrgRepositoryRef(p.domain, owner, repoName)
		orgRepo, err := p.provider.OrgRepositories().Get(ctx, orgRef)

		if err != nil {
			return fmt.Errorf("error getting org repo reference for owner %s, repo %s, %s ", owner, repoName, err)
		}

		fmt.Println("uploading deploy key")

		_, err = orgRepo.DeployKeys().Create(ctx, deployKeyInfo)
		if err != nil {
			return fmt.Errorf("error uploading deploy key %s", err)
		}

		if err = utils.WaitUntil(os.Stdout, time.Second, defaultTimeout, func() error {
			_, err = orgRepo.DeployKeys().Get(ctx, deployKeyName)
			return err
		}); err != nil {
			return fmt.Errorf("error verifying deploy key %s existance for repo %s. %s", deployKeyName, repoName, err)
		}
	case AccountTypeUser:
		userRef := NewUserRepositoryRef(p.domain, owner, repoName)
		userRepo, err := p.provider.UserRepositories().Get(ctx, userRef)

		if err != nil {
			return fmt.Errorf("error getting user repo reference for owner %s, repo %s, %s ", owner, repoName, err)
		}

		fmt.Println("uploading deploy key")

		_, err = userRepo.DeployKeys().Create(ctx, deployKeyInfo)
		if err != nil {
			return fmt.Errorf("error uploading deploy key %s", err)
		}

		if err = utils.WaitUntil(os.Stdout, time.Second, defaultTimeout, func() error {
			_, err = userRepo.DeployKeys().Get(ctx, deployKeyName)
			return err
		}); err != nil {
			return fmt.Errorf("error verifying deploy key %s existance for repo %s. %s", deployKeyName, repoName, err)
		}
	default:
		return fmt.Errorf("account type not supported %s", ownerType)
	}

	return nil
}

func (p defaultGitProvider) GetAccountType(owner string) (ProviderAccountType, error) {
	ctx := context.Background()
	defer ctx.Done()

	_, err := p.provider.Organizations().Get(ctx, gitprovider.OrganizationRef{
		Domain:       p.domain,
		Organization: owner,
	})

	if err != nil {
		if errors.Is(err, gitprovider.ErrNotFound) || strings.Contains(err.Error(), gitprovider.ErrGroupNotFound.Error()) {
			return AccountTypeUser, nil
		}

		return "", fmt.Errorf("could not get account type %s", err)
	}

	return AccountTypeOrg, nil
}

func (p defaultGitProvider) GetDefaultBranch(url string) (string, error) {
	repoInfoRef, err := p.GetRepoInfoFromUrl(url)

	if err != nil {
		return "", err
	}

	if repoInfoRef != nil {
		repoInfo := *repoInfoRef
		if repoInfo.DefaultBranch != nil {
			return *repoInfo.DefaultBranch, nil
		}
	}

	return "main", nil
}

func (p defaultGitProvider) GetRepoVisibility(url string) (*gitprovider.RepositoryVisibility, error) {
	repoInfoRef, err := p.GetRepoInfoFromUrl(url)

	if err != nil {
		return nil, err
	}

	return getVisibilityFromRepoInfo(url, repoInfoRef)
}

func getVisibilityFromRepoInfo(url string, repoInfoRef *gitprovider.RepositoryInfo) (*gitprovider.RepositoryVisibility, error) {
	if repoInfoRef != nil {
		repoInfo := *repoInfoRef
		if repoInfo.Visibility != nil {
			return repoInfo.Visibility, nil
		}
	}

	return nil, fmt.Errorf("unable to obtain repository visibility for: %s", url)
}

func (p defaultGitProvider) GetRepoInfoFromUrl(repoUrl string) (*gitprovider.RepositoryInfo, error) {
	normalizedUrl, err := NewNormalizedRepoURL(repoUrl)
	if err != nil {
		return nil, fmt.Errorf("error normalizing url: %w", err)
	}

	accountType, err := p.GetAccountType(normalizedUrl.owner)
	if err != nil {
		return nil, err
	}

	repoInfo, err := p.GetRepoInfo(accountType, normalizedUrl.owner, normalizedUrl.repoName)
	if err != nil {
		return nil, err
	}

	return repoInfo, nil
}

func (p defaultGitProvider) GetRepoInfo(accountType ProviderAccountType, owner string, repoName string) (*gitprovider.RepositoryInfo, error) {
	ctx := context.Background()
	defer ctx.Done()

	switch accountType {
	case AccountTypeOrg:
		repo, err := p.GetOrgRepo(owner, repoName)
		if err != nil {
			return nil, err
		}

		info := repo.Get()

		return &info, nil
	case AccountTypeUser:
		repo, err := p.GetUserRepo(owner, repoName)
		if err != nil {
			return nil, err
		}

		info := repo.Get()

		return &info, nil
	default:
		return nil, fmt.Errorf("unexpected account type %s", accountType)
	}
}

func (p defaultGitProvider) GetOrgRepo(org string, repoName string) (gitprovider.OrgRepository, error) {
	ctx := context.Background()
	defer ctx.Done()

	orgRepoRef := NewOrgRepositoryRef(p.domain, org, repoName)

	repo, err := p.provider.OrgRepositories().Get(ctx, orgRepoRef)
	if err != nil {
		return nil, fmt.Errorf("error getting org repository %w", err)
	}

	return repo, nil
}

func (p defaultGitProvider) GetUserRepo(user string, repoName string) (gitprovider.UserRepository, error) {
	ctx := context.Background()
	defer ctx.Done()

	userRepoRef := NewUserRepositoryRef(p.domain, user, repoName)

	repo, err := p.provider.UserRepositories().Get(ctx, userRepoRef)
	if err != nil {
		return nil, fmt.Errorf("error getting user repository %w", err)
	}

	return repo, nil
}

func (p defaultGitProvider) CreateOrgRepository(orgRepoRef gitprovider.OrgRepositoryRef, repoInfo gitprovider.RepositoryInfo, opts ...gitprovider.RepositoryCreateOption) error {
	ctx := context.Background()
	defer ctx.Done()

	_, err := p.provider.OrgRepositories().Create(ctx, orgRepoRef, repoInfo, opts...)
	if err != nil {
		return fmt.Errorf("error creating repo %w", err)
	}

	return p.waitUntilRepoCreated(AccountTypeOrg, orgRepoRef.Organization, orgRepoRef.RepositoryName)
}

func (p defaultGitProvider) CreateUserRepository(userRepoRef gitprovider.UserRepositoryRef, repoInfo gitprovider.RepositoryInfo, opts ...gitprovider.RepositoryCreateOption) error {
	ctx := context.Background()
	defer ctx.Done()

	_, err := p.provider.UserRepositories().Create(ctx, userRepoRef, repoInfo, opts...)
	if err != nil {
		return fmt.Errorf("error creating repo %s", err)
	}

	return p.waitUntilRepoCreated(AccountTypeUser, userRepoRef.UserLogin, userRepoRef.RepositoryName)
}

func (p defaultGitProvider) CreatePullRequestToUserRepo(userRepRef gitprovider.UserRepositoryRef, targetBranch string, newBranch string, files []gitprovider.CommitFile, commitMessage string, prTitle string, prDescription string) (gitprovider.PullRequest, error) {
	ctx := context.Background()

	ur, err := p.provider.UserRepositories().Get(ctx, userRepRef)
	if err != nil {
		return nil, fmt.Errorf("error getting info for repo [%s] err [%s]", userRepRef.String(), err)
	}

	if targetBranch == "" {
		targetBranch = *ur.Get().DefaultBranch
	}

	commits, err := ur.Commits().ListPage(ctx, targetBranch, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("error getting commits for repo[%s] err [%s]", userRepRef.String(), err)
	}

	if len(commits) == 0 {
		return nil, fmt.Errorf("targetBranch [%s] does not exists", targetBranch)
	}

	latestCommit := commits[0]

	if err := ur.Branches().Create(ctx, newBranch, latestCommit.Get().Sha); err != nil {
		return nil, fmt.Errorf("error creating branch [%s] for repo [%s] err [%s]", newBranch, userRepRef.String(), err)
	}

	if _, err := ur.Commits().Create(ctx, newBranch, commitMessage, files); err != nil {
		return nil, fmt.Errorf("error creating commit for branch [%s] for repo [%s] err [%s]", newBranch, userRepRef.String(), err)
	}

	pr, err := ur.PullRequests().Create(ctx, prTitle, newBranch, targetBranch, prDescription)
	if err != nil {
		return nil, fmt.Errorf("error creating pull request [%s] for branch [%s] for repo [%s] err [%s]", prTitle, newBranch, userRepRef.String(), err)
	}

	return pr, nil
}

func (p defaultGitProvider) CreatePullRequestToOrgRepo(orgRepRef gitprovider.OrgRepositoryRef, targetBranch string, newBranch string, files []gitprovider.CommitFile, commitMessage string, prTitle string, prDescription string) (gitprovider.PullRequest, error) {
	ctx := context.Background()

	ur, err := p.provider.OrgRepositories().Get(ctx, orgRepRef)
	if err != nil {
		return nil, fmt.Errorf("error getting info for repo [%s] err [%s]", orgRepRef.String(), err)
	}

	if targetBranch == "" {
		targetBranch = *ur.Get().DefaultBranch
	}

	commits, err := ur.Commits().ListPage(ctx, targetBranch, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("error getting commits for repo [%s] err [%s]", orgRepRef.String(), err)
	}

	if len(commits) == 0 {
		return nil, fmt.Errorf("targetBranch [%s] does not exists", targetBranch)
	}

	latestCommit := commits[0]

	if err := ur.Branches().Create(ctx, newBranch, latestCommit.Get().Sha); err != nil {
		return nil, fmt.Errorf("error creating branch [%s] for repo [%s] err [%s]", newBranch, orgRepRef.String(), err)
	}

	if _, err := ur.Commits().Create(ctx, newBranch, commitMessage, files); err != nil {
		return nil, fmt.Errorf("error creating commit for branch [%s] for repo [%s] err [%s]", newBranch, orgRepRef.String(), err)
	}

	pr, err := ur.PullRequests().Create(ctx, prTitle, newBranch, targetBranch, prDescription)
	if err != nil {
		return nil, fmt.Errorf("error creating pull request [%s] for branch [%s] for repo [%s] err [%s]", prTitle, newBranch, orgRepRef.String(), err)
	}

	return pr, nil
}

// GetCommitsFromUserRepo gets a limit of 10 commits from a user repo
func (p defaultGitProvider) GetCommitsFromUserRepo(userRepRef gitprovider.UserRepositoryRef, targetBranch string, pageSize int, pageToken int) ([]gitprovider.Commit, error) {
	ctx := context.Background()

	ur, err := p.provider.UserRepositories().Get(ctx, userRepRef)
	if err != nil {
		return nil, fmt.Errorf("error getting info for repo [%s] err [%s]", userRepRef.String(), err)
	}

	// currently locking the commit list at 10. May discuss pagination options later.
	commits, err := ur.Commits().ListPage(ctx, targetBranch, pageSize, pageToken)
	if err != nil {
		return nil, fmt.Errorf("error getting commits for repo [%s] err [%s]", userRepRef.String(), err)
	}

	return commits, nil
}

// GetCommitsFromUserRepo gets a limit of 10 commits from an organization
func (p defaultGitProvider) GetCommitsFromOrgRepo(orgRepRef gitprovider.OrgRepositoryRef, targetBranch string, pageSize int, pageToken int) ([]gitprovider.Commit, error) {
	ctx := context.Background()

	ur, err := p.provider.OrgRepositories().Get(ctx, orgRepRef)
	if err != nil {
		return nil, fmt.Errorf("error getting info for repo [%s] err [%s]", orgRepRef.String(), err)
	}

	// currently locking the commit list at 10. May discuss pagination options later.
	commits, err := ur.Commits().ListPage(ctx, targetBranch, pageSize, pageToken)
	if err != nil {
		return nil, fmt.Errorf("error getting commits for repo [%s] err [%s]", orgRepRef.String(), err)
	}

	return commits, nil
}

func (p defaultGitProvider) GetProviderDomain() string {
	return string(GitProviderName(p.provider.ProviderID())) + ".com"
}

func NewRepositoryInfo(description string, visibility gitprovider.RepositoryVisibility) gitprovider.RepositoryInfo {
	return gitprovider.RepositoryInfo{
		Description: &description,
		Visibility:  &visibility,
	}
}

func NewOrgRepositoryRef(domain, org, repoName string) gitprovider.OrgRepositoryRef {
	return gitprovider.OrgRepositoryRef{
		RepositoryName: repoName,
		OrganizationRef: gitprovider.OrganizationRef{
			Domain:       domain,
			Organization: org,
		},
	}
}

func NewUserRepositoryRef(domain, user, repoName string) gitprovider.UserRepositoryRef {
	return gitprovider.UserRepositoryRef{
		RepositoryName: repoName,
		UserRef: gitprovider.UserRef{
			Domain:    domain,
			UserLogin: user,
		},
	}
}

func (p defaultGitProvider) waitUntilRepoCreated(ownerType ProviderAccountType, owner, name string) error {
	if err := utils.WaitUntil(os.Stdout, time.Second, defaultTimeout, func() error {
		_, err := p.GetRepoInfo(ownerType, owner, name)
		return err
	}); err != nil {
		return fmt.Errorf("could not verify repo existence %s", err)
	}

	return nil
}

// DetectGitProviderFromUrl accepts a url related to a git repo and
// returns the name of the provider associated.
// The raw URL is assumed to be something like ssh://git@github.com/myorg/myrepo.git.
// The common `git clone` variant of `git@github.com:myorg/myrepo.git` is not supported.
func DetectGitProviderFromUrl(raw string) (GitProviderName, error) {
	// Needed for url parse to work for some urls
	if strings.HasPrefix(raw, "git@") {
		raw = "ssh://" + raw
		raw = strings.Replace(raw, ".com:", ".com/", 1)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("could not parse git repo url %q", raw)
	}

	switch u.Hostname() {
	case "github.com":
		return GitProviderGitHub, nil
	case "gitlab.com":
		return GitProviderGitLab, nil
	}

	return "", fmt.Errorf("no git providers found for \"%s\"", raw)
}

type RepositoryURLProtocol string

const RepositoryURLProtocolHTTPS RepositoryURLProtocol = "https"
const RepositoryURLProtocolSSH RepositoryURLProtocol = "ssh"

type NormalizedRepoURL struct {
	repoName   string
	owner      string
	url        *url.URL
	normalized string
	provider   GitProviderName
	protocol   RepositoryURLProtocol
}

// normalizeRepoURLString accepts a url like git@github.com:someuser/podinfo.git and converts it into
// a string like ssh://git@github.com/someuser/podinfo.git. This helps standardize the different
// user inputs that might be provided.
func normalizeRepoURLString(url string, providerName string) string {
	trimmed := ""

	if !strings.HasSuffix(url, ".git") {
		url = url + ".git"
	}

	sshPrefix := fmt.Sprintf("git@%s.com:", providerName)
	if strings.HasPrefix(url, sshPrefix) {
		trimmed = strings.TrimPrefix(url, sshPrefix)
	}

	httpsPrefix := fmt.Sprintf("https://%s.com/", providerName)
	if strings.HasPrefix(url, httpsPrefix) {
		trimmed = strings.TrimPrefix(url, httpsPrefix)
	}

	if trimmed != "" {
		return fmt.Sprintf("ssh://git@%s.com/%s", providerName, trimmed)
	}

	return url
}

func NewNormalizedRepoURL(uri string) (NormalizedRepoURL, error) {
	providerName, err := DetectGitProviderFromUrl(uri)
	if err != nil {
		return NormalizedRepoURL{}, fmt.Errorf("could get provider name from URL %s: %w", uri, err)
	}

	normalized := normalizeRepoURLString(uri, string(providerName))

	u, err := url.Parse(normalized)
	if err != nil {
		return NormalizedRepoURL{}, fmt.Errorf("could not create normalized repo URL %s: %w", uri, err)
	}

	owner, err := getOwnerFromUrl(normalized, providerName)
	if err != nil {
		return NormalizedRepoURL{}, fmt.Errorf("could get owner name from URL %s: %w", uri, err)
	}

	protocol := RepositoryURLProtocolSSH
	if u.Scheme == "https" {
		protocol = RepositoryURLProtocolHTTPS
	}

	return NormalizedRepoURL{
		repoName:   utils.UrlToRepoName(uri),
		owner:      owner,
		url:        u,
		normalized: normalized,
		provider:   providerName,
		protocol:   protocol,
	}, nil
}

func (n NormalizedRepoURL) String() string {
	return n.normalized
}

func (n NormalizedRepoURL) URL() *url.URL {
	return n.url
}

func (n NormalizedRepoURL) Owner() string {
	return n.owner
}

func (n NormalizedRepoURL) RepositoryName() string {
	return n.repoName
}

func (n NormalizedRepoURL) Provider() GitProviderName {
	return n.provider
}

func (n NormalizedRepoURL) Protocol() RepositoryURLProtocol {
	return n.protocol
}

func getOwnerFromUrl(url string, providerName GitProviderName) (string, error) {
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("could not get owner from url %s", url)
	}

	// Used to detect if a gitlab subgroup is used
	if providerName == GitProviderGitLab {
		if !strings.Contains(parts[len(parts)-3], "gitlab.com") {
			return parts[len(parts)-3] + "/" + parts[len(parts)-2], nil
		}
	}

	return parts[len(parts)-2], nil
}
