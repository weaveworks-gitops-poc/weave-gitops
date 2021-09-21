package unpause

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/weaveworks/weave-gitops/cmd/gitops/version"
	"github.com/weaveworks/weave-gitops/pkg/apputils"
	"github.com/weaveworks/weave-gitops/pkg/kube"
	"github.com/weaveworks/weave-gitops/pkg/services/app"
	"k8s.io/apimachinery/pkg/types"
)

var params app.UnpauseParams

var Cmd = &cobra.Command{
	Use:           "unpause <app-name>",
	Short:         "Unpause an application",
	Args:          cobra.MinimumNArgs(1),
	Example:       "gitops app unpause podinfo",
	RunE:          runCmd,
	SilenceUsage:  true,
	SilenceErrors: true,
	PostRun: func(cmd *cobra.Command, args []string) {
		version.CheckVersion(version.CheckpointParamsWithFlags(version.CheckpointParams(), cmd))
	},
}

func runCmd(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	params.Namespace, _ = cmd.Parent().Flags().GetString("namespace")
	params.Name = args[0]

	kube, _, err := kube.NewKubeHTTPClient()
	if err != nil {
		return fmt.Errorf("failed to create kube client: %w", err)
	}

	appObj, err := kube.GetApplication(ctx, types.NamespacedName{Name: params.Name, Namespace: params.Namespace})
	if err != nil {
		return fmt.Errorf("could not get application: %w", err)
	}

	appService, appError := apputils.GetAppService(ctx, appObj.Spec.URL, appObj.Spec.ConfigURL, appObj.Namespace, appObj.IsHelmRepository())
	if appError != nil {
		return fmt.Errorf("failed to create app service: %w", appError)
	}

	if err := appService.Unpause(params); err != nil {
		return errors.Wrapf(err, "failed to unpause the app %s", params.Name)
	}

	return nil
}
