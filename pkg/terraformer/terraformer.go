// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/pkg/utils"
)

const (
	// maxPatchRetries define the maximum number of attempts to patch a resource in case of conflict
	maxPatchRetries = 2

	// terraformVersionKey is the terraform version key in the terraform state JSON
	terraformVersionKey = "terraform_version"
	// registry is the terraform registry
	registry = "registry.terraform.io"
	// version012Prefix is the prefix for 0.12 versions
	version012Prefix = "0.12."
)

var (
	// TerraformBinary is the name of the terraform binary, it allows to overwrite it for testing purposes
	TerraformBinary = "terraform"

	// Stdout alias to os.Stdout allowing output redirection in tests
	Stdout io.Writer = os.Stdout

	// Stderr alias to os.Stderr allowing output redirection in tests
	Stderr io.Writer = os.Stderr

	// SignalNotify allows mocking signal.Notify in tests
	SignalNotify = signal.Notify
)

var (
	// providers is a map from the short provider names (used with terraform 0.12 or lower)
	// to their sources (used with terraform 0.13 or higher)
	providers = map[string]string{
		"aws":         "hashicorp/aws",
		"azurerm":     "hashicorp/azurerm",
		"google":      "hashicorp/google",
		"google-beta": "hashicorp/google-beta",
		"openstack":   "terraform-provider-openstack/openstack",
		"alicloud":    "hashicorp/alicloud",
		"template":    "hashicorp/template",
		"null":        "hashicorp/null",
	}
)

// NewDefaultTerraformer creates a new Terraformer with the default PathSet and logger.
func NewDefaultTerraformer(config *Config) (*Terraformer, error) {
	return NewTerraformer(config, runtimelog.Log, DefaultPaths().WithBaseDir(config.BaseDir), clock.RealClock{})
}

// NewTerraformer creates a new Terraformer with the given options.
func NewTerraformer(config *Config, log logr.Logger, paths *PathSet, clock clock.Clock) (*Terraformer, error) {
	t := &Terraformer{
		config: config,
		log:    log,
		paths:  paths,
		clock:  clock,

		StateUpdateQueue: workqueue.NewNamedRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(10*time.Millisecond, 5*time.Minute), "state-update"),
		// use buffered channel, to make sure we don't miss the signal
		FinalStateUpdateSucceeded: make(chan struct{}, 1),
	}

	c, err := client.New(config.RESTConfig, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	t.client = c

	return t, nil
}

// InjectClient allows injecting a mock client for some test cases.
func (t *Terraformer) InjectClient(client client.Client) {
	t.client = client
}

// Run starts to terraformer execution with the given terraform command.
func (t *Terraformer) Run(command Command) error {
	if _, ok := SupportedCommands[command]; !ok {
		return fmt.Errorf("terraform command %q is not supported", command)
	}

	t.log.V(1).Info("executing terraformer with config", "config", t.config)

	return t.execute(command)
}

// execute is the main function of terraformer and puts all parts together (interacting with the terraform config and
// state resources on the kubernetes cluster, executing and watching terraform calls, delegating process signals and
// watching the state file).
func (t *Terraformer) execute(command Command) (rErr error) {
	sigCh := make(chan os.Signal, 1)
	SignalNotify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-sigCh:
			t.log.Info("interrupt received")
			cancel()
		case <-ctx.Done():
		}
	}()

	if command == Destroy {
		// Sometimes a state is empty because the Terraformer has never run successfully.
		// Hence, we take a shortcut here and just remove the finalizer.
		emptyState, err := t.isStateEmpty(ctx)
		if err != nil {
			return fmt.Errorf("failed to check if state is empty: %w", err)
		}

		if emptyState {
			return t.removeFinalizer()
		}
	}

	if err := t.EnsureTFDirs(); err != nil {
		return fmt.Errorf("failed to create needed directories: %w", err)
	}

	if err := t.FetchConfigAndState(ctx); err != nil {
		return err
	}

	shutdownWorker := t.StartStateUpdateWorker()
	defer shutdownWorker()

	// always try to update state ConfigMap one last time before exiting
	defer func() {
		err := t.TriggerAndWaitForFinalStateUpdate()
		if rErr == nil {
			// make sure, we don't exit with exit code 0, if we were unable to store the state
			rErr = err
		}
	}()

	// start file watcher for state file, that will continuously update state configmap
	// as soon as state file changes on disk
	shutdownFileWatcher, err := t.StartFileWatcher()
	if err != nil {
		return fmt.Errorf("failed to start state file watcher: %w", err)
	}

	// stop file watcher and wait for it to be finished
	defer shutdownFileWatcher()

	if err := t.addFinalizer(ctx); err != nil {
		return fmt.Errorf("error adding finalizers: %w", err)
	}

	// initialize terraform plugins
	if err := t.executeTerraform(ctx, Init); err != nil {
		return fmt.Errorf("error executing terraform %s: %w", Init, err)
	}

	// get terraform version from state and execute state replace-provider commands if needed
	terraformVersion, err := t.getTerraformVersionFromState(ctx)
	if err != nil {
		return fmt.Errorf("error getting terraform version from state: %w", err)
	}
	if strings.HasPrefix(terraformVersion, version012Prefix) {
		for name, source := range providers {
			fromProvider, toProvider := fmt.Sprintf("%s/-/%s", registry, name), fmt.Sprintf("%s/%s", registry, source)
			if err := t.executeTerraform(ctx, StateReplaceProvider, fromProvider, toProvider); err != nil {
				return fmt.Errorf("error executing terraform %s %s %s: %w", StateReplaceProvider, fromProvider, toProvider, err)
			}
		}
	}

	// execute main terraform command
	if err := t.executeTerraform(ctx, command); err != nil {
		return fmt.Errorf("error executing terraform %s: %w", command, err)
	}

	if command == Validate {
		if err := t.executeTerraform(ctx, Plan); err != nil {
			return fmt.Errorf("error executing terraform %s: %w", Plan, err)
		}
	}

	// after a successful execution of destroy command, remove the finalizers from the resources
	if command == Destroy {
		if err := t.removeFinalizer(); err != nil {
			return fmt.Errorf("error removing finalizers: %w", err)
		}
	}

	return nil
}

func (t *Terraformer) executeTerraform(ctx context.Context, command Command, params ...string) error {
	log := t.stepLogger("executeTerraform")

	// open termination log file already to ensure we can write to it. If we can't write to it, we should exit early
	// instead of running terraform from which we can't properly transport the failure logs
	terminationLogFile, err := os.OpenFile(t.paths.TerminationMessagePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer terminationLogFile.Close()

	var args []string
	if command == StateReplaceProvider {
		args = append(args, strings.Split(string(command), " ")...)
	} else {
		args = append(args, "-chdir="+t.paths.ConfigDir)
		args = append(args, string(command))
	}

	// disable colors, which will look weird in termination message, k8s status fields and so on
	args = append(args, "-no-color")

	switch command {
	case Init:
	case Plan:
		args = append(args, "-var-file="+t.paths.VarsPath, "-parallelism=4", "-detailed-exitcode", "-state="+t.paths.StatePath)
	case Apply:
		args = append(args, "-var-file="+t.paths.VarsPath, "-parallelism=4", "-auto-approve", "-state="+t.paths.StatePath)
	case Destroy:
		args = append(args, "-var-file="+t.paths.VarsPath, "-parallelism=4", "-auto-approve", "-state="+t.paths.StatePath)
	case StateReplaceProvider:
		args = append(args, "-auto-approve", "-state="+t.paths.StatePath)
		args = append(args, params...)
	}

	log.Info("executing terraform", "command", command, "args", strings.Join(args, " "))
	tfCmd := exec.Command(TerraformBinary, args...) // #nosec: G204 -- the variable is only referring to subcommands of the hardcoded executable. Since the full command had to be constructed dynamically, this is needed.

	logBuffer := &bytes.Buffer{}
	terraformOutput := io.MultiWriter(Stderr, logBuffer)
	// redirect all terraform output to stderr (same as logs) and temporary buffer
	tfCmd.Stdout = terraformOutput
	tfCmd.Stderr = terraformOutput

	if err := tfCmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	// wait for signal handler goroutine to finish properly before returning
	defer wg.Wait()

	doneCh := make(chan struct{})
	defer close(doneCh)

	// setup signal handler relaying signals to terraform process
	go func() {
		defer wg.Done()
		select {
		case <-doneCh:
			return
		case <-ctx.Done():
			log.V(1).Info("relaying interrupt to terraform process")
			if err := tfCmd.Process.Signal(syscall.SIGINT); err != nil {
				log.Error(err, "failed to relay interrupt to terraform process")
			}
		}
	}()

	if err := tfCmd.Wait(); err != nil {
		log.Error(err, "terraform process finished with error", "command", command)

		// copy terraform logs to termination log file for error code detection
		if _, copyErr := io.Copy(terminationLogFile, logBuffer); copyErr != nil {
			// don't return copy error here to transport the execution error
			log.Error(copyErr, "failed to copy terraform logs to termination log", "terminationLogFile", terminationLogFile)
		}

		return utils.WithExitCode{Code: tfCmd.ProcessState.ExitCode(), Underlying: err}
	}

	log.Info("terraform process finished successfully", "command", command)
	return nil
}

func (t *Terraformer) addFinalizer(ctx context.Context) error {
	logger := t.stepLogger("add-finalizer")
	return t.updateObjects(ctx, logger, controllerutil.AddFinalizer)

}

func (t *Terraformer) removeFinalizer() error {
	// root context might have been cancelled during terraform execution, but execution was still successful.
	// use a new background context here, otherwise the finalizers can't be removed
	finalizerCtx, finalizerCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer finalizerCancel()

	logger := t.stepLogger("remove-finalizer")
	return t.updateObjects(finalizerCtx, logger, controllerutil.RemoveFinalizer)
}

func (t *Terraformer) isStateEmpty(ctx context.Context) (bool, error) {
	state := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.config.StateConfigMapName,
			Namespace: t.config.Namespace,
		},
	}
	if err := t.client.Get(ctx, client.ObjectKeyFromObject(state), state); client.IgnoreNotFound(err) != nil {
		return false, err
	}
	data, ok := state.Data[tfStateKey]
	return !ok || len(data) == 0, nil
}

func (t *Terraformer) getTerraformVersionFromState(ctx context.Context) (string, error) {
	state := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.config.StateConfigMapName,
			Namespace: t.config.Namespace,
		},
	}
	if err := t.client.Get(ctx, client.ObjectKeyFromObject(state), state); client.IgnoreNotFound(err) != nil {
		return "", err
	}
	data, ok := state.Data[tfStateKey]
	if !ok || len(data) == 0 {
		return "", nil
	}
	var terraformState map[string]interface{}
	if err := json.Unmarshal([]byte(data), &terraformState); err != nil {
		return "", fmt.Errorf("could not unmarshal terraform state from JSON: %w", err)
	}
	terraformVersion, ok := terraformState[terraformVersionKey].(string)
	if !ok {
		return "", fmt.Errorf("terraform state key %s is not of type string", terraformVersionKey)
	}
	return terraformVersion, nil
}

func (t *Terraformer) terraformObjects() []client.Object {
	return []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: t.config.Namespace,
				Name:      t.config.VariablesSecretName,
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: t.config.Namespace,
				Name:      t.config.ConfigurationConfigMapName,
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: t.config.Namespace,
				Name:      t.config.StateConfigMapName,
			},
		},
	}
}

func (t *Terraformer) updateObjects(ctx context.Context, log logr.Logger, patchObj func(client.Object, string) bool) error {
	allErrors := &multierror.Error{
		ErrorFormat: utils.NewErrorFormatFuncWithPrefix("failed to update object finalizer"),
	}

	log.Info("updating finalizers for terraform resources")
	for _, obj := range t.terraformObjects() {
		if err := t.updateObjectFinalizers(ctx, log, obj, patchObj); err != nil {
			allErrors = multierror.Append(allErrors, err)
		}
	}

	err := allErrors.ErrorOrNil()
	if err != nil {
		log.Error(err, "failed to updated finalizers for all terraform resources")
	} else {
		log.Info("successfully updated finalizers for terraform resources")
	}
	return err
}

func (t *Terraformer) updateObjectFinalizers(ctx context.Context, log logr.Logger, obj client.Object, patchObj func(client.Object, string) bool) error {
	var (
		key = client.ObjectKeyFromObject(obj)
		err error
	)

	for i := 0; i < maxPatchRetries; i++ {
		err = t.client.Get(ctx, key, obj)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.V(1).Info("create empty object", "key", key)
				patchObj(obj, TerraformerFinalizer)
				return t.client.Create(ctx, obj)
			}
			log.Error(err, "failed to get object", "key", key)
			return err
		}

		old := (obj.DeepCopyObject()).(client.Object)
		patchObj(obj, TerraformerFinalizer)
		err = t.client.Patch(ctx, obj, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{}))
		if !apierrors.IsConflict(err) {
			break
		}
	}

	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "failed to update object in the store", "key", key)
		return err
	}

	return nil
}
