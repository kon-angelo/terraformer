// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"io/ioutil"
	"os"

	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = Describe("Options", func() {
	var (
		opts *Options
	)

	BeforeEach(func() {
		opts = NewOptions()
	})

	Describe("#NewOptions", func() {
		It("should create an empty Options object", func() {
			Expect(opts).To(gstruct.PointTo(Equal(Options{})))
		})
	})

	Describe("#Complete", func() {
		var (
			configurationConfigMapName = "example.infra.tf-config"
			stateConfigMapName         = "example.infra.tf-state"
			variablesSecretName        = "example.infra.tf-vars"
			namespace                  = "fancy-namespace"

			tempKubeconfigFile   string
			revertTempKubeconfig func()
		)

		BeforeEach(func() {
			// create temp kubeconfig file for each test individually to avoid test pollution
			tempKubeconfigFile, revertTempKubeconfig = createTempKubeconfig()

			opts.configurationConfigMapName = configurationConfigMapName
			opts.stateConfigMapName = stateConfigMapName
			opts.variablesSecretName = variablesSecretName
			opts.namespace = namespace
			opts.kubeconfig = tempKubeconfigFile
		})

		AfterEach(func() {
			revertTempKubeconfig()
		})

		It("should successfully complete options", func() {
			Expect(opts.Complete()).To(Succeed())
		})

		Context("flag validation", func() {
			It("should use the given resource names", func() {
				Expect(opts.Complete()).To(Succeed())

				completed := opts.Completed()
				Expect(completed.ConfigurationConfigMapName).To(Equal(configurationConfigMapName))
				Expect(completed.StateConfigMapName).To(Equal(stateConfigMapName))
				Expect(completed.VariablesSecretName).To(Equal(variablesSecretName))
			})
			It("should use empty base dir if omitted", func() {
				opts.baseDir = ""
				Expect(opts.Complete()).To(Succeed())

				completed := opts.Completed()
				Expect(completed.BaseDir).To(Equal(""))
			})
			It("should use the given base dir", func() {
				baseDir := "/tmp/foo/bar"
				opts.baseDir = baseDir
				Expect(opts.Complete()).To(Succeed())

				completed := opts.Completed()
				Expect(completed.BaseDir).To(Equal(baseDir))
			})
			It("should fail if --configuration-configmap-name is unset", func() {
				opts.configurationConfigMapName = ""
				Expect(opts.Complete()).To(MatchError(ContainSubstring("configuration-configmap-name")))
			})
			It("should fail if --state-configmap-name is unset", func() {
				opts.stateConfigMapName = ""
				Expect(opts.Complete()).To(MatchError(ContainSubstring("state-configmap-name")))
			})
			It("should fail if --variables-secret-name is unset", func() {
				opts.variablesSecretName = ""
				Expect(opts.Complete()).To(MatchError(ContainSubstring("variables-secret-name")))
			})
		})

		Context("REST config validation", func() {
			var (
				revertKubeconfigEnvVar func()
			)

			BeforeEach(func() {
				// unset KUBECONFIG env var to prepare a clean test environment,
				// as most developers probably have it set in their environment
				revertKubeconfigEnvVar = test.WithEnvVar("KUBECONFIG", "")
			})

			AfterEach(func() {
				revertKubeconfigEnvVar()
			})

			It("should fail, if neither flag nor env var is set", func() {
				opts.kubeconfig = ""
				Expect(opts.Complete()).To(MatchError(ContainSubstring("invalid configuration: no configuration has been provided")))
			})
			It("should use kubeconfig provided via flag", func() {
				opts.kubeconfig = tempKubeconfigFile
				Expect(opts.Complete()).To(Succeed())
				Expect(opts.Completed().RESTConfig).To(matchTempRESTConfig())
			})
			It("should use kubeconfig provided via env var if flag is not given", func() {
				opts.kubeconfig = ""
				defer test.WithEnvVar("KUBECONFIG", tempKubeconfigFile)()
				Expect(opts.Complete()).To(Succeed())
				Expect(opts.Completed().RESTConfig).To(matchTempRESTConfig())
			})
		})

		Context("namespace validation", func() {
			It("should use default namespace if neither flag nor env var is given", func() {
				opts.namespace = ""
				Expect(opts.Complete()).To(Succeed())
				Expect(opts.Completed().Namespace).To(Equal("default"))
			})
			It("should use namespace provided via flag", func() {
				opts.namespace = "apple"
				Expect(opts.Complete()).To(Succeed())
				Expect(opts.Completed().Namespace).To(Equal("apple"))
			})
			It("should use namespace provided via env var if flag is not given", func() {
				opts.namespace = ""
				defer test.WithEnvVar("NAMESPACE", "peach")()
				Expect(opts.Complete()).To(Succeed())
				Expect(opts.Completed().Namespace).To(Equal("peach"))
			})
		})
	})
})

const (
	kubeconfigTemplate = `apiVersion: v1
kind: Config
current-context: local-garden
clusters:
- name: local-garden
  cluster:
    server: https://localhost:2443
contexts:
- name: local-garden
  context:
    cluster: local-garden
    user: local-garden
users:
- name: local-garden
  user:
    token: foo
`
)

// createTempKubeconfig creates a temporary kubeconfig file from the template.
// It returns the path of the temporary file and a cleanup function to delete the file.
func createTempKubeconfig() (string, func()) {
	file, err := ioutil.TempFile("", "kubeconfig-test.yaml")
	Expect(err).NotTo(HaveOccurred())

	Expect(ioutil.WriteFile(file.Name(), []byte(kubeconfigTemplate), 0644)).To(Succeed())
	Expect(file.Close()).To(Succeed())

	return file.Name(), func() {
		Expect(os.Remove(file.Name())).To(Succeed())
	}
}

func matchTempRESTConfig() types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Host":        Equal("https://localhost:2443"),
		"BearerToken": Equal("foo"),
	}))
}
