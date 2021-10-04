package apputils

import (
	"context"
	"fmt"

	wego "github.com/weaveworks/weave-gitops/api/v1alpha1"
	"github.com/weaveworks/weave-gitops/pkg/flux"
	"github.com/weaveworks/weave-gitops/pkg/git"
	"github.com/weaveworks/weave-gitops/pkg/gitproviders"
	"github.com/weaveworks/weave-gitops/pkg/kube"
	"github.com/weaveworks/weave-gitops/pkg/logger"
	"github.com/weaveworks/weave-gitops/pkg/osys"
	"github.com/weaveworks/weave-gitops/pkg/runner"
	"github.com/weaveworks/weave-gitops/pkg/services/app"
	"github.com/weaveworks/weave-gitops/pkg/services/auth"
	"k8s.io/apimachinery/pkg/types"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate . AppFactory
type AppFactory interface {
	GetKubeService() (kube.Kube, error)
	GetAppService(ctx context.Context, name, namespace string) (app.AppService, error)
}

type DefaultAppFactory struct {
}

func (f *DefaultAppFactory) GetAppService(ctx context.Context, name, namespace string) (app.AppService, error) {
	return GetAppService(ctx, name, namespace)
}

func (f *DefaultAppFactory) GetKubeService() (kube.Kube, error) {
	kubeClient, _, err := kube.NewKubeHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("error creating k8s http client: %w", err)
	}

	return kubeClient, nil
}

func GetLogger() logger.Logger {
	osysClient := osys.New()
	return logger.NewCLILogger(osysClient.Stdout())
}

func GetBaseClients() (osys.Osys, flux.Flux, kube.Kube, logger.Logger, error) {
	osysClient := osys.New()
	cliRunner := &runner.CLIRunner{}
	fluxClient := flux.New(osysClient, cliRunner)

	kubeClient, _, err := kube.NewKubeHTTPClient()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error creating k8s http client: %w", err)
	}

	logger := logger.NewCLILogger(osysClient.Stdout())

	return osysClient, fluxClient, kubeClient, logger, nil
}

func IsClusterReady() error {
	logger := GetLogger()

	kube, _, err := kube.NewKubeHTTPClient()
	if err != nil {
		return fmt.Errorf("error creating k8s http client: %w", err)
	}

	return app.IsClusterReady(logger, kube)
}

func GetAppService(ctx context.Context, appName string, namespace string) (app.AppService, error) {
	osysClient, fluxClient, kubeClient, logger, err := GetBaseClients()
	if err != nil {
		return nil, fmt.Errorf("error initializing clients: %w", err)
	}

	appClient, configClient, gitProvider, err := getGitClientsForApp(ctx, appName, namespace, false)
	if err != nil {
		return nil, fmt.Errorf("error getting git clients: %w", err)
	}

	return app.New(ctx, logger, appClient, configClient, gitProvider, fluxClient, kubeClient, osysClient), nil
}

func GetAppServiceForAdd(ctx context.Context, url, configUrl, namespace string, isHelmRepository bool, dryRun bool) (app.AppService, error) {
	osysClient, fluxClient, kubeClient, logger, err := GetBaseClients()
	if err != nil {
		return nil, fmt.Errorf("error initializing clients: %w", err)
	}

	appClient, configClient, gitProvider, err := getGitClients(ctx, url, configUrl, namespace, isHelmRepository, dryRun)
	if err != nil {
		return nil, fmt.Errorf("error getting git clients: %w", err)
	}

	return app.New(ctx, logger, appClient, configClient, gitProvider, fluxClient, kubeClient, osysClient), nil
}

func getGitClientsForApp(ctx context.Context, appName string, namespace string, dryRun bool) (git.Git, git.Git, gitproviders.GitProvider, error) {
	kube, _, err := kube.NewKubeHTTPClient()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating k8s http client: %w", err)
	}

	app, err := kube.GetApplication(ctx, types.NamespacedName{Namespace: namespace, Name: appName})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not retrieve application %q: %w", appName, err)
	}

	isHelmRepository := app.Spec.SourceType == wego.SourceTypeHelm

	return getGitClients(ctx, app.Spec.URL, app.Spec.ConfigURL, namespace, isHelmRepository, dryRun)
}

func getGitClients(ctx context.Context, url, configUrl, namespace string, isHelmRepository bool, dryRun bool) (git.Git, git.Git, gitproviders.GitProvider, error) {
	isExternalConfig := app.IsExternalConfigUrl(configUrl)

	var providerUrl string

	switch {
	case !isHelmRepository:
		providerUrl = url
	case isExternalConfig:
		providerUrl = configUrl
	default:
		return nil, nil, nil, nil
	}

	normalizedUrl, err := gitproviders.NewNormalizedRepoURL(providerUrl)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error normalizing url: %w", err)
	}

	kube, _, err := kube.NewKubeHTTPClient()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating k8s http client: %w", err)
	}

	targetName, err := kube.GetClusterName(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error getting target name: %w", err)
	}

	authsvc, err := GetAuthService(ctx, normalizedUrl, dryRun)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating auth service: %w", err)
	}

	var appClient, configClient git.Git

	if !isHelmRepository {
		// We need to do this even if we have an external config to set up the deploy key for the app repo
		appRepoClient, appRepoErr := authsvc.CreateGitClient(ctx, normalizedUrl, targetName, namespace)
		if appRepoErr != nil {
			return nil, nil, nil, appRepoErr
		}

		appClient = appRepoClient
	}

	if isExternalConfig {
		normalizedConfigUrl, err := gitproviders.NewNormalizedRepoURL(configUrl)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error normalizing url: %w", err)
		}

		configRepoClient, configRepoErr := authsvc.CreateGitClient(ctx, normalizedConfigUrl, targetName, namespace)
		if configRepoErr != nil {
			return nil, nil, nil, configRepoErr
		}

		configClient = configRepoClient
	} else {
		configClient = appClient
	}

	return appClient, configClient, authsvc.GetGitProvider(), nil
}

func GetAuthService(ctx context.Context, normalizedUrl gitproviders.NormalizedRepoURL, dryRun bool) (auth.AuthService, error) {
	var (
		gitProvider gitproviders.GitProvider
		err         error
	)

	if dryRun {
		if gitProvider, err = gitproviders.NewDryRun(); err != nil {
			return nil, fmt.Errorf("error creating git provider client: %w", err)
		}
	} else {
		if gitProvider, err = auth.GetGitProvider(ctx, normalizedUrl); err != nil {
			return nil, fmt.Errorf("error obtaining git provider token: %w", err)
		}
	}

	osysClient := osys.New()
	cliRunner := &runner.CLIRunner{}
	fluxClient := flux.New(osysClient, cliRunner)
	logger := logger.NewCLILogger(osysClient.Stdout())

	_, rawClient, err := kube.NewKubeHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("error creating k8s http client: %w", err)
	}

	return auth.NewAuthService(fluxClient, rawClient, gitProvider, logger)
}
