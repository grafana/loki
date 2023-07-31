package main

import (
	"context"
	goflag "flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/rest"
	utilflag "k8s.io/component-base/cli/flag"
	logs "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"

	helloworld "open-cluster-management.io/addon-framework/examples/helloworld"
	// "open-cluster-management.io/addon-framework/examples/helloworld_agent"
	"github.com/grafana/loki/operator/hack/klusterlet-addon/loki"
	"open-cluster-management.io/addon-framework/examples/helloworld_hosted"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	cmdfactory "open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/addon-framework/pkg/version"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.AddFlags(logs.NewLoggingConfiguration(), pflag.CommandLine)

	command := newCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addon",
		Short: "multi cluster logging addon",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
			os.Exit(1)
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(newControllerCommand())
	// TODO this will agent work
	// cmd.AddCommand(helloworld_agent.NewAgentCommand(helloworld_hosted.AddonName))
	// cmd.AddCommand(helloworld_agent.NewCleanupAgentCommand(helloworld_hosted.AddonName))

	return cmd
}

func newControllerCommand() *cobra.Command {
	cmd := cmdfactory.
		NewControllerCommandConfig("logging-addon-controller", version.Get(), runController).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the multi cluster logging addon controller"

	return cmd
}

func runController(ctx context.Context, kubeConfig *rest.Config) error {
	mgr, err := addonmanager.New(kubeConfig)
	if err != nil {
		return err
	}
	registrationOption := helloworld.NewRegistrationOption(
		kubeConfig,
		helloworld_hosted.AddonName,
		utilrand.String(5))

	agentAddon, err := addonfactory.NewAgentAddonFactory(
		loki.AddonName, loki.FS, "manifests").
		WithGetValuesFuncs(loki.GetDefaultValues, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(registrationOption).
		WithAgentHostedModeEnabledOption().
		// WithInstallStrategy(addonagent.InstallAllStrategy(helloworld.InstallationNamespace)).
		// WithAgentHealthProber(loki.AgentHealthProber()).
		BuildTemplateAgentAddon()
	if err != nil {
		klog.Errorf("failed to build agent %v", err)
		return err
	}

	err = mgr.AddAgent(agentAddon)
	if err != nil {
		klog.Fatal(err)
	}

	err = mgr.Start(ctx)
	if err != nil {
		klog.Fatal(err)
	}
	<-ctx.Done()

	return nil
}
