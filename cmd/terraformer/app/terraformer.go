// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"errors"
	"flag"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	terraformercmd "github.com/gardener/terraformer/pkg/cmd"
	"github.com/gardener/terraformer/pkg/terraformer"
	versionpkg "github.com/gardener/terraformer/pkg/version"
)

// NewTerraformerCommand creates a new terraformer cobra command
func NewTerraformerCommand() *cobra.Command {
	var (
		version = versionpkg.Get()
		zapOpts = &logzap.Options{}
		tfOpts  = terraformercmd.NewOptions()
	)

	// root command
	cmd := &cobra.Command{
		Use:   "terraformer",
		Short: "terraformer executes terraform commands inside a Kubernetes cluster",
		Long: `terraformer executes terraform commands inside a Kubernetes cluster and handles Pod lifecycle event (e.g. shutdown signals).
It reads and stores terraform config and state from/to Kubernetes resources (ConfigMaps and Secrets).
Also, it continuously updates the state ConfigMap by running a file watcher and updating the ConfigMap as soon as the state file changes.`,
		Example: exampleForCommand(""),
		Version: fmt.Sprintf("%#v", version),
		Hidden:  true,

		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			runtimelog.SetLogger(logzap.New(
				// use configuration passed via flags
				logzap.UseFlagOptions(zapOpts),
				// and overwrite some stuff
				func(o *logzap.Options) {
					if !o.Development {
						encCfg := zap.NewProductionEncoderConfig()
						// overwrite time encoding to human readable format for production logs
						encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
						o.Encoder = zapcore.NewJSONEncoder(encCfg)
					}

					// don't print stacktrace for warning level logs
					o.StacktraceLevel = zapcore.ErrorLevel
				},
			))

			runtimelog.Log.Info("Starting terraformer...", "version", version.GitVersion, "provider", version.Provider)
			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				runtimelog.Log.Info(fmt.Sprintf("FLAG: --%s=%s", flag.Name, flag.Value))
			})
		},

		RunE: func(_ *cobra.Command, _ []string) error {
			// root command called without any terraform command is invalid and should exit with error code
			return errors.New("no command was specified")
		},
	}
	cmd.SetUsageTemplate(cosmeticUsageTemplate)

	// setup a subcommand for every supported terraform command
	for command := range terraformer.SupportedCommands {
		addSubcommand(cmd, command, tfOpts)
	}

	// setup flags
	tfOpts.AddFlags(cmd.PersistentFlags())
	zapOpts.BindFlags(flag.CommandLine)
	cmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	return cmd
}

func addSubcommand(cmd *cobra.Command, command terraformer.Command, opts *terraformercmd.Options) {
	cmd.AddCommand(&cobra.Command{
		Use:     string(command),
		Short:   fmt.Sprintf("execute `terraform %s`", command),
		Long:    fmt.Sprintf("terraformer %s executes the terraform %s command with the given configuration", command, command),
		Args:    cobra.NoArgs,
		Example: exampleForCommand(string(command)),

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Complete(); err != nil {
				return err
			}

			// don't output usage on further errors raised during terraform execution
			cmd.SilenceUsage = true
			// further errors will be logged properly, don't duplicate
			cmd.SilenceErrors = true

			tf, err := terraformer.NewDefaultTerraformer(opts.Completed())
			if err != nil {
				return err
			}

			return tf.Run(command)
		},
	})
}

// custom usage template:
// - exclude `terraformer [flags]` if command is hidden (e.g. root command)
// - indicate [flags] after `terraformer [command]` (for subcommands)
const cosmeticUsageTemplate = `Usage:{{if and .Runnable (not .Hidden)}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command] [flags]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

func exampleForCommand(command string) string {
	if command == "" {
		command = "apply"
	}

	return "terraformer " + command + ` \
  --configuration-configmap-name=example.infra.tf-config \
  --state-configmap-name=example.infra.tf-state \
  --variables-secret-name=example.infra.tf-vars`
}
