// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer

import (
	"os"
	"path"
	"path/filepath"

	"github.com/go-logr/logr"
)

// PathSet carries the set of file paths for terraform files and allows to set different paths in tests
type PathSet struct {
	// ConfigDir is the directory to hold the main terraform scripts (`main.tf` and `variables.tf`)
	ConfigDir string
	// VarsDir is the directory to hold the terraform variables values file (`terraform.tfvars`)
	VarsDir string
	// StateDir is the directory to hold the terraform state file (`terraform.tfstate`)
	StateDir string
	// ProvidersDir is the directory which contains the provider plugin binaries
	ProvidersDir string

	// TerminationMessagePath is the file, which the termination log should be written to.
	// Should be used as spec.containers[].terminationMessagePath in the terraformer pod spec,
	// see https://kubernetes.io/docs/tasks/debug-application-cluster/determine-reason-pod-failure/
	TerminationMessagePath string

	// VarsPath is the complete path the the variables values file
	VarsPath string
	// StatePath is the complete path the the state file
	StatePath string
}

// DefaultPaths returns the default PathSet used in terraformer
func DefaultPaths() *PathSet {
	p := &PathSet{
		ConfigDir:              "/tf",
		VarsDir:                "/tfvars",
		StateDir:               "/tfstate",
		ProvidersDir:           "/terraform-providers",
		TerminationMessagePath: "/terraform-termination-log",
	}
	p.VarsPath = path.Join(p.VarsDir, tfVarsKey)
	p.StatePath = path.Join(p.StateDir, tfStateKey)

	return p
}

// WithBaseDir returns a copy of the PathSet with all paths rooted in baseDir.
// This is used for testing purposes to use paths located e.g. under temporary directories.
func (p *PathSet) WithBaseDir(baseDir string) *PathSet {
	return &PathSet{
		ConfigDir:              filepath.Join(baseDir, p.ConfigDir),
		VarsDir:                filepath.Join(baseDir, p.VarsDir),
		StateDir:               filepath.Join(baseDir, p.StateDir),
		ProvidersDir:           filepath.Join(baseDir, p.ProvidersDir),
		TerminationMessagePath: filepath.Join(baseDir, p.TerminationMessagePath),
		VarsPath:               filepath.Join(baseDir, p.VarsPath),
		StatePath:              filepath.Join(baseDir, p.StatePath),
	}
}

// EnsureDirs ensures that the needed directories for the terraform files are present.
func (p *PathSet) EnsureDirs(log logr.Logger) error {
	log.Info("ensuring terraform directories")

	for _, dir := range []string{
		p.ConfigDir,
		p.VarsDir,
		p.StateDir,
	} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
		log.V(1).Info("directory ensured", "dir", dir)
	}
	return nil
}
