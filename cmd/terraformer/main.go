// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/cmd/terraformer/app"
	"github.com/gardener/terraformer/pkg/terraformer"
	"github.com/gardener/terraformer/pkg/utils"
)

func main() {
	if err := exec.Command("which", terraformer.TerraformBinary).Run(); // #nosec: G204 -- The variable is a hardcoded string and is only modified in tests.
	err != nil {
		panic("terraform is not installed or not executable. cannot start terraformer.")
	}

	if err := app.NewTerraformerCommand().Execute(); err != nil {
		if log := runtimelog.Log; log.Enabled() {
			log.Error(err, "error running terraformer")
		} else {
			fmt.Printf("error running terraformer: %v", err)
		}

		// set exit code from terraform
		withExitCode := &utils.WithExitCode{}
		if errors.As(err, withExitCode) {
			if exitCode := withExitCode.ExitCode(); exitCode > 0 {
				os.Exit(exitCode)
			}
		}
		os.Exit(1)
	}
}
