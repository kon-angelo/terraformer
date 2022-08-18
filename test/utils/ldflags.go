// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// TerraformerPackage is the terraformer package path
	TerraformerPackage = "github.com/gardener/terraformer"
)

// Overwrite is a variable overwrite which can be passed to -ldflags
type Overwrite struct {
	VarPath, Value string
}

func (o Overwrite) String() string {
	return fmt.Sprintf("-X %s=%s", o.VarPath, o.Value)
}

// LDFlags returns ldflags to pass to `go build` or `gexec.Build` in tests
func LDFlags(overwrites ...Overwrite) []string {
	if len(overwrites) == 0 {
		return []string{}
	}

	var ldflags string
	for _, o := range overwrites {
		ldflags += " " + o.String()
	}
	return []string{"-ldflags", ldflags}
}

// OverwriteTerraformBinary returns an overwrite for the terraform binary name
func OverwriteTerraformBinary(path string) Overwrite {
	return Overwrite{
		VarPath: TerraformerPackage + "/pkg/terraformer.TerraformBinary",
		Value:   path,
	}
}

// OverwriteExitCode returns an overwrite that configures the binary to always exit with the given code.
func OverwriteExitCode(code string) Overwrite {
	return Overwrite{
		VarPath: "main.expectedExitCodes",
		Value:   code,
	}
}

// OverwriteExitCodeForCommands returns an overwrite that configures the binary to exit with the given code if it is
// invoked with the given command. Example usage:
//	testutils.OverwriteExitCodeForCommands(
//		"init", "0",
//		"apply", "42",
//		"destroy", "43",
//	),
func OverwriteExitCodeForCommands(commandAndCodes ...string) Overwrite {
	if len(commandAndCodes)%2 != 0 {
		panic(fmt.Errorf("len of commandAndCodes should be even but is %d", len(commandAndCodes)))
	}

	var combined string
	for i := 0; i < len(commandAndCodes); i += 2 {
		combined += commandAndCodes[i] + "=" + commandAndCodes[i+1] + ","
	}

	return Overwrite{
		VarPath: "main.expectedExitCodes",
		Value:   combined,
	}
}

// OverwriteSleepDuration returns an overwrite for the exit code var
func OverwriteSleepDuration(code string) Overwrite {
	return Overwrite{
		VarPath: "main.sleepDuration",
		Value:   code,
	}
}

// HashBuildArgs returns a hash for an arbitrary set of build args.
// This allows us to detect, if a test binary really needs to be rebuild or if we can reuse the same binary from the
// last build. This way we can significantly shorten the runtime of the binary e2e tests, which build terraformer for
// each test case.
func HashBuildArgs(args interface{}) (string, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// CanReuseLastBuild calculates a hash for the given build args and compares it the the value, hashVar points to.
// If they are equal, it returns true, indicating, that the last build can be reused for this test run.
// Otherwise, it returns false and sets hashVar to the calculated hash.
func CanReuseLastBuild(binaryName string, hashVar *string, args interface{}) bool {
	if hashVar == nil {
		Fail("hashVar must not be nil")
	}

	newHash, err := HashBuildArgs(args)
	Expect(err).NotTo(HaveOccurred())
	_, err = fmt.Fprintf(GinkgoWriter, "%s build hash: %s\n", binaryName, newHash)
	Expect(err).NotTo(HaveOccurred())

	if *hashVar != "" && *hashVar == newHash {
		_, err := fmt.Fprintf(GinkgoWriter, "reusing last %s build, as hash hasn't changed\n", binaryName)
		Expect(err).NotTo(HaveOccurred())
		return true
	}
	*hashVar = newHash
	return false
}
