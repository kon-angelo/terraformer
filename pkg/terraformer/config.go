// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/terraformer/pkg/utils"
)

const (
	tfConfigMainKey = "main.tf"
	tfConfigVarsKey = "variables.tf"
	tfVarsKey       = "terraform.tfvars"
	tfStateKey      = "terraform.tfstate"
)

// EnsureTFDirs ensures that the needed directories for the terraform files are present.
func (t *Terraformer) EnsureTFDirs() error {
	return t.paths.EnsureDirs(t.stepLogger("EnsureTFDirs"))
}

// FetchConfigAndState fetches the needed config and state objects from the Kubernetes API and stores their contents
// in separate files.
func (t *Terraformer) FetchConfigAndState(ctx context.Context) error {
	log := t.stepLogger("FetchConfigAndState")

	var (
		wg      wait.Group
		allErrs = &multierror.Error{
			ErrorFormat: utils.NewErrorFormatFuncWithPrefix("failed to fetch terraform config"),
		}
		errCh = make(chan error)
	)

	wg.Start(func() {
		errCh <- fetchConfigMap(ctx, log, t.client, t.config.Namespace, t.config.ConfigurationConfigMapName, false,
			t.paths.ConfigDir, tfConfigMainKey, tfConfigVarsKey,
		)
	})
	wg.Start(func() {
		errCh <- fetchConfigMap(ctx, log, t.client, t.config.Namespace, t.config.StateConfigMapName, true,
			t.paths.StateDir, tfStateKey,
		)
	})
	wg.Start(func() {
		errCh <- fetchSecret(ctx, log, t.client, t.config.Namespace, t.config.VariablesSecretName, false,
			t.paths.VarsDir, tfVarsKey,
		)
	})

	go func() {
		defer close(errCh)
		wg.Wait()
	}()

	for err := range errCh {
		allErrs = multierror.Append(allErrs, err)
	}

	return allErrs.ErrorOrNil()
}

func fetchConfigMap(ctx context.Context, log logr.Logger, c client.Client, ns, name string, optional bool, dir string, dataKeys ...string) error {
	return fetchObject(ctx, log, c, "ConfigMap", ns, name, &ConfigMapStore{&corev1.ConfigMap{}}, optional, dir, dataKeys...)
}

func fetchSecret(ctx context.Context, log logr.Logger, c client.Client, ns, name string, optional bool, dir string, dataKeys ...string) error {
	return fetchObject(ctx, log, c, "Secret", ns, name, &SecretStore{&corev1.Secret{}}, optional, dir, dataKeys...)
}

func fetchObject(ctx context.Context, log logr.Logger, c client.Client, kind, ns, name string, obj Store, optional bool, dir string, dataKeys ...string) error {
	key := client.ObjectKey{Namespace: ns, Name: name}
	log = log.WithValues("kind", kind, "object", key, "dir", dir)
	log.V(1).Info("fetching object")

	if err := c.Get(ctx, key, obj.Object()); err != nil {
		if apierrors.IsNotFound(err) && optional {
			log.V(1).Info("object not found but optional")

			// if terraformer container was restarted, the state file should always be truncated and filled with the contents
			// of the state ConfigMap (which is the single source of truth)
			for _, dataKey := range dataKeys {
				filePath := filepath.Join(dir, dataKey)
				log.V(1).Info("creating empty file / truncating existing file", "dataKey", dataKey, "file", filePath)
				file, err := os.OpenFile(filepath.Clean(filePath), os.O_CREATE|os.O_TRUNC, 0600)
				if err != nil {
					return err
				}
				_ = file.Close()
			}

			return nil
		}
		return err
	}

	for _, dataKey := range dataKeys {
		filePath := filepath.Join(dir, dataKey)

		if err := func() error {
			var notFoundErr KeyNotFoundError
			reader, err := obj.Read(dataKey)
			if errors.As(err, &notFoundErr) && optional {
				log.V(1).Info("key not found but object is optional, creating empty file / truncating existing file", "dataKey", dataKey, "file", filePath)
				file, err := os.OpenFile(filepath.Clean(filePath), os.O_CREATE|os.O_TRUNC, 0600)
				if err != nil {
					return err
				}
				return file.Close()
			}
			if err != nil {
				return fmt.Errorf("failed reading from %s %q: %w", kind, key, err)
			}

			log.V(1).Info("copying contents to file", "dataKey", dataKey, "file", filePath)
			file, err := os.OpenFile(filepath.Clean(filePath), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(file, reader)
			return err
		}(); err != nil {
			return err
		}
	}

	return nil
}
