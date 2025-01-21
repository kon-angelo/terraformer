// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer

import (
	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Command is a terraform command
type Command string

// known terraform commands
const (
	// Init is the terraform `init` command.
	Init Command = "init"
	// Apply is the terraform `apply` command.
	Apply Command = "apply"
	// Destroy is the terraform `destroy` command.
	Destroy Command = "destroy"
	// Validate is the terraform `validate` command.
	Validate Command = "validate"
	// Plan is the terraform `plan` command.
	Plan Command = "plan"
	// StateReplaceProvider is the terraform `state` command with the `replace-provider` subcommand.
	StateReplaceProvider Command = "state replace-provider"
)

const (
	// TerraformerFinalizer is the finalizer used by the terraformer on the terraform configmaps and secrets
	TerraformerFinalizer = "gardener.cloud/terraformer"
)

// SupportedCommands contains the set of supported terraform commands, that can be run as `terraformer <command>`.
var SupportedCommands = map[Command]struct{}{
	Apply:    {},
	Destroy:  {},
	Validate: {},
}

// Terraformer can execute terraform commands and fetch/store config and state from/into Secrets/ConfigMaps
type Terraformer struct {
	config *Config
	paths  *PathSet
	log    logr.Logger

	client client.Client

	// StateUpdateQueue is the queue in which file watch events are inserted to trigger a state update.
	// It is also used for triggering the final state update.
	StateUpdateQueue workqueue.RateLimitingInterface
	// FinalStateUpdateSucceeded is a channel over which a value will be send by the state update worker
	// to signal that the final state update has succeeded and terraformer can safely exit.
	FinalStateUpdateSucceeded chan struct{}

	// clock allows faking some time operations in tests
	clock clock.Clock
}

// Config holds configuration options for Terraformer.
type Config struct {
	// ConfigurationConfigMapName is the name of the ConfigMap that holds the `main.tf` and `variables.tf` files.
	ConfigurationConfigMapName string
	// StateConfigMapName is the name of the ConfigMap that the `terraform.tfstate` file should be stored in.
	StateConfigMapName string
	// VariablesSecretName is the name of the Secret that holds the `terraform.tfvars` file.
	VariablesSecretName string
	// Namespace is the namespace to store the configuration resources in.
	Namespace string

	// RESTConfig holds the completed rest.Config.
	RESTConfig *rest.Config

	// Client is the kubernetes client. Used mostly for testing. If nil, the client will be constructed from RESTConfig.
	Client client.Client

	// BaseDir is the base directory to be used for all terraform files (defaults to '/').
	BaseDir string
}

// MarshalLogObject implements zapcore.ObjectMarshaler.
func (c *Config) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("configurationConfigMapName", c.ConfigurationConfigMapName)
	enc.AddString("stateConfigMapName", c.StateConfigMapName)
	enc.AddString("variablesSecretName", c.VariablesSecretName)
	enc.AddString("namespace", c.Namespace)
	return nil
}
