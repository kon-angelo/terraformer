// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"os"
	"path"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

const (
	// FakeTerraformBinary is the name of the fake terraform binary
	FakeTerraformBinary = "fake-terraform"
)

var (
	fakeTerraformBinPath       string
	fakeTerraformBuildArgsHash string
)

// FakeTerraform models the terraform fake binary
type FakeTerraform struct {
	// Path is the path to the fake terraform binary
	Path string
}

// NewFakeTerraform builds a new terraform fake binary, which can be used to mock terraform executions in tests
func NewFakeTerraform(overwrites ...Overwrite) FakeTerraform {
	By("building fake terraform")
	if fakeTerraformBinPath != "" && CanReuseLastBuild("fake terraform", &fakeTerraformBuildArgsHash, overwrites) {
		return FakeTerraform{fakeTerraformBinPath}
	}

	var err error
	fakeTerraformBinPath, err = gexec.Build(
		TerraformerPackage+"/test/utils/fake-terraform",
		LDFlags(overwrites...)...,
	)
	Expect(err).NotTo(HaveOccurred())

	return FakeTerraform{fakeTerraformBinPath}
}

// TerraformerEnv returns the environment variables to use for executing terraformer, so that it will be able to find
// the fake terraform binary.
func (f FakeTerraform) TerraformerEnv() []string {
	var env []string

	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "PATH="):
			env = append(env, "PATH="+path.Dir(f.Path)+":"+strings.TrimPrefix(e, "PATH="))
		case strings.HasPrefix(e, "KUBECONFIG="):
			// don't pass KUBECONFIG env var down implicitly
			continue
		default:
			env = append(env, e)
		}
	}

	return env
}
