// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// stateUpdateTimeout is the timeout of a single state update call
	stateUpdateTimeout = 2 * time.Minute
	// FinalStateUpdateTimeout is the overall timeout for waiting for the final state update to succeed
	// (including retries with exponential backoff)
	FinalStateUpdateTimeout = time.Hour
)

const (
	// FinalStateUpdateKey is a key, which will be added to the state-update queue to trigger the final state update.
	// It indicates, that the state update should be retried on any error (i.e. the worker should add the key back
	// to the queue after the update failed).
	FinalStateUpdateKey = iota
	// ContinuousStateUpdateKey is a key, which will be added to the state-update queue to trigger a state update during
	// runtime of terraformer, i.e. on changes to the state file. It indicates, that the state doesn't need to be
	// retried (i.e. the worker should not add the key back to the queue).
	ContinuousStateUpdateKey
)

// StoreState stores the state file in the configured state ConfigMap.
// It uses a hard timeout of 2m and doesn't retry the update on any error.
func (t *Terraformer) StoreState(ctx context.Context) error {
	return storeConfigMap(ctx, t.log, t.client, t.config.Namespace, t.config.StateConfigMapName, t.paths.StateDir, tfStateKey)
}

func storeConfigMap(ctx context.Context, log logr.Logger, c client.Client, ns, name, dir string, dataKeys ...string) error {
	return storeObject(ctx, log.WithValues("kind", "ConfigMap"), c, &ConfigMapStore{&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}}, dir, dataKeys...)
}

func storeObject(ctx context.Context, log logr.Logger, c client.Client, obj Store, dir string, dataKeys ...string) error {
	key := client.ObjectKey{Namespace: obj.Object().GetNamespace(), Name: obj.Object().GetName()}
	log = log.WithValues("object", key)

	// rather timeout after 2m (and retry) instead of hanging in a non-progressing connection
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, stateUpdateTimeout)
	defer cancel()

	for _, dataKey := range dataKeys {
		if err := func() error {
			file, err := os.Open(filepath.Clean(filepath.Join(dir, dataKey)))
			if err != nil {
				return err
			}
			defer file.Close()
			log.V(1).Info("copying file content into object", "dataKey", dataKey, "file", file.Name())

			return obj.Store(dataKey, file)
		}(); err != nil {
			return err
		}
	}

	log.V(1).Info("storing object")

	if err := func() error {
		// try patch first and fallback to create if the object doesn't exist
		// to avoid always sending two requests in the "normal" case
		// this will reduce API calls for storing the state by roughly 1/2
		err := c.Patch(ctx, obj.Object(), client.Merge)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return c.Create(ctx, obj.Object())
			}
		}
		return err
	}(); err != nil {
		return err
	}

	log.V(1).Info("successfully updated object")
	return nil
}

// StartStateUpdateWorker starts a worker goroutine, that will read from the state-update queue and call StoreState for
// every item. It returns a func that should be executed as part of the shutdown procedure, which shuts down the
// workqueue and the worker and then waits until the worker has finished the last work item.
func (t *Terraformer) StartStateUpdateWorker() func() {
	log := t.log.WithName("state-update-queue")

	var wg wait.Group
	ctx, cancel := context.WithCancel(context.Background())

	wg.StartWithContext(ctx, func(ctx context.Context) {
		wait.Until(t.stateUpdateWorker, time.Second, ctx.Done())
	})

	return func() {
		log.V(1).Info("shutting down state-update queue")
		// shutdown queue -> no further items can be added (i.e. also retries will be stopped)
		t.StateUpdateQueue.ShutDown()

		// wait for the queue to be empty
		for {
			remaining := t.StateUpdateQueue.Len()
			if remaining == 0 {
				break
			}
			log.Info("waiting for state-update queue to be empty", "remainingItems", remaining)
			t.clock.Sleep(5 * time.Second)
		}

		// stop worker and wait for graceful termination (one state update might still be in process),
		// once last item was dequeued, we will have to wait until the update call succeeds or times out.
		cancel()
		wg.Wait()
	}
}

func (t *Terraformer) stateUpdateWorker() {
	for t.processNextStateUpdate() {
	}
}

func (t *Terraformer) processNextStateUpdate() bool {
	log := t.log.WithName("state-update-worker")
	key, quit := t.StateUpdateQueue.Get()
	if quit {
		log.V(1).Info("queue is empty and shutting down, stopping work")
		return false
	}
	defer t.StateUpdateQueue.Done(key)

	isFinalStateUpdate := key == FinalStateUpdateKey
	log.V(1).Info("processing work item", "withRetries", isFinalStateUpdate)

	// run StoreState in background, context of the worker will be cancelled at end of terraformer execution
	// StoreState itself configures a timeout for the API calls
	if err := t.StoreState(context.Background()); err != nil {
		log.Error(err, "error storing state")
		if isFinalStateUpdate {
			log.V(1).Info("adding item back to queue with backoff after error")
			t.StateUpdateQueue.AddRateLimited(key)
			return true
		}
	}
	t.StateUpdateQueue.Forget(key)

	if isFinalStateUpdate {
		// signal that final state update has succeeded and we can safely exit
		select {
		case t.FinalStateUpdateSucceeded <- struct{}{}:
		default: // don't block the worker, if the receiver of the channel is not ready
		}
	}

	return true
}

// StartFileWatcher watches the state file for changes and stores the file contents in the state ConfigMap as soon as
// the file gets updated.
// It returns a func that should be executed as part of the shutdown procedure, which stops the file watcher and
// waits for the event handler goroutine to finish.
func (t *Terraformer) StartFileWatcher() (func(), error) {
	log := t.log.WithName("file-watcher")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	var wg wait.Group
	// file watcher should run in background and should only be cancelled when this function returns, i.e. when any
	// running terraform processes have finished
	ctx, cancel := context.WithCancel(context.Background())

	wg.StartWithContext(ctx, func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				log.V(1).Info("stopping file watcher")
				_ = watcher.Close()
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				fileLog := log.WithValues("file", event.Name)
				fileLog.V(1).Info("received event for file", "op", event.Op.String())

				if event.Name == t.paths.StatePath && event.Op&fsnotify.Write == fsnotify.Write {
					fileLog.V(1).Info("triggering state update")
					t.StateUpdateQueue.Add(ContinuousStateUpdateKey)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error(err, "error while watching state file")
			}
		}
	})

	log.Info("starting file watcher for state file", "file", t.paths.StatePath)
	if err := watcher.Add(t.paths.StatePath); err != nil {
		cancel()
		return nil, err
	}

	return func() {
		cancel()
		wg.Wait()
	}, nil
}

// TriggerAndWaitForFinalStateUpdate triggers the final state update and waits until it has succeeded or timed out.
func (t *Terraformer) TriggerAndWaitForFinalStateUpdate() error {
	log := t.stepLogger("finalStateUpdate")
	log.Info("triggering final state update before exiting", "timeout", FinalStateUpdateTimeout.String())
	t.StateUpdateQueue.Add(FinalStateUpdateKey)

	// wait until final state update has succeeded or timeout has occurred
	select {
	case <-t.clock.After(FinalStateUpdateTimeout):
		err := fmt.Errorf("timed out waiting for final state update to complete")
		log.Error(err, "error updating state")
		log.Info("logging contents of state file to stdout as last resort")
		if err2 := t.LogStateContentsToStdout(); err2 != nil {
			log.Error(err2, "failed copying state contents to stdout, now things are messed up and you probably need to cleanup manually :(")
		}
		return err
	case <-t.FinalStateUpdateSucceeded:
	}

	log.Info("successfully stored terraform state")
	return nil
}

// LogStateContentsToStdout copies the contents of the state file to Stdout.
// This is the last resort in case we couldn't update the state ConfigMap before timing out (e.g. in catastrophic
// situations where the API server is unavailable for over 1h). Maybe the logs can help in such situations to recover
// the state.
func (t *Terraformer) LogStateContentsToStdout() error {
	file, err := os.Open(t.paths.StatePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(Stdout, file)
	return err
}
