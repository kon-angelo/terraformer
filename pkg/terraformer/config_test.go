// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/pkg/terraformer"
	testutils "github.com/gardener/terraformer/test/utils"
)

var _ = Describe("Terraformer Config", func() {
	var (
		tf       *terraformer.Terraformer
		baseDir  string
		paths    *terraformer.PathSet
		testObjs *testutils.TestObjects
	)

	BeforeEach(func() {
		var err error
		baseDir, err = ioutil.TempDir("", "tf-test-*")
		Expect(err).NotTo(HaveOccurred())

		var handle testutils.CleanupActionHandle
		handle = testutils.AddCleanupAction(func() {
			defer testutils.RemoveCleanupAction(handle)
			Expect(os.RemoveAll(baseDir)).To(Succeed())
		})

		paths = terraformer.DefaultPaths().WithBaseDir(baseDir)

		testObjs = testutils.PrepareTestObjects(ctx, testClient, "", "")

		tf, err = terraformer.NewTerraformer(
			&terraformer.Config{
				Namespace:                  testObjs.Namespace,
				ConfigurationConfigMapName: testObjs.ConfigurationConfigMap.Name,
				StateConfigMapName:         testObjs.StateConfigMap.Name,
				VariablesSecretName:        testObjs.VariablesSecret.Name,
				RESTConfig:                 restConfig,
			},
			runtimelog.Log,
			paths,
			clock.RealClock{},
		)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		testutils.RunCleanupActions()
	})

	Describe("#EnsureDirs", func() {
		It("should create directories successfully", func() {
			Expect(tf.EnsureTFDirs()).To(Succeed())
		})
	})

	Describe("#FetchConfigAndState", func() {
		BeforeEach(func() {
			Expect(tf.EnsureTFDirs()).To(Succeed())
		})

		It("should fetch all objects if present", func() {
			Expect(tf.FetchConfigAndState(ctx)).To(Succeed())

			contents, err := ioutil.ReadFile(filepath.Join(paths.ConfigDir, testutils.ConfigMainKey))
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(BeEquivalentTo(testObjs.ConfigurationConfigMap.Data[testutils.ConfigMainKey]))
			contents, err = ioutil.ReadFile(filepath.Join(paths.ConfigDir, testutils.ConfigVarsKey))
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(BeEquivalentTo(testObjs.ConfigurationConfigMap.Data[testutils.ConfigVarsKey]))

			contents, err = ioutil.ReadFile(paths.StatePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(BeEquivalentTo(testObjs.StateConfigMap.Data[testutils.StateKey]))

			contents, err = ioutil.ReadFile(paths.VarsPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(BeEquivalentTo(testObjs.VariablesSecret.Data[testutils.VarsKey]))
		})

		Context("config fetching", func() {
			It("should fail if config ConfigMap is not present", func() {
				Expect(testClient.Delete(ctx, testObjs.ConfigurationConfigMap)).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(MatchError(ContainSubstring("not found")))
			})
			It("should fail if main config key is not present", func() {
				delete(testObjs.ConfigurationConfigMap.Data, testutils.ConfigMainKey)
				Expect(testClient.Update(ctx, testObjs.ConfigurationConfigMap)).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(MatchError(ContainSubstring("not found")))
			})
			It("should fail if vars config key is not present", func() {
				delete(testObjs.ConfigurationConfigMap.Data, testutils.ConfigVarsKey)
				Expect(testClient.Update(ctx, testObjs.ConfigurationConfigMap)).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(MatchError(ContainSubstring("not found")))
			})
		})

		Context("variables fetching", func() {
			It("should fail if variables secret is not present", func() {
				Expect(testClient.Delete(ctx, testObjs.VariablesSecret)).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(MatchError(ContainSubstring("not found")))
			})
			It("should fail if vars key is not present", func() {
				delete(testObjs.VariablesSecret.Data, testutils.VarsKey)
				Expect(testClient.Update(ctx, testObjs.VariablesSecret)).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(MatchError(ContainSubstring("not found")))
			})
		})

		Context("state fetching", func() {
			It("should create empty state file if state ConfigMap is not present", func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, testObjs.StateConfigMap))).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(Succeed())

				contents, err := ioutil.ReadFile(paths.StatePath)
				Expect(err).NotTo(HaveOccurred(), "state file should be present")
				Expect(contents).To(BeEmpty(), "state file should be empty")
			})
			It("should truncate existing state file if state ConfigMap is not present", func() {
				Expect(ioutil.WriteFile(paths.StatePath, []byte("state from last run"), 0644)).To(Succeed())

				Expect(client.IgnoreNotFound(testClient.Delete(ctx, testObjs.StateConfigMap))).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(Succeed())

				contents, err := ioutil.ReadFile(paths.StatePath)
				Expect(err).NotTo(HaveOccurred(), "state file should be present")
				Expect(contents).To(BeEmpty(), "state file should be empty")
			})
			It("should create empty state file if state key is not present", func() {
				delete(testObjs.StateConfigMap.Data, testutils.StateKey)
				Expect(testClient.Update(ctx, testObjs.StateConfigMap)).To(Succeed())

				Expect(tf.FetchConfigAndState(ctx)).To(Succeed())

				contents, err := ioutil.ReadFile(paths.StatePath)
				Expect(err).NotTo(HaveOccurred(), "state file should be present")
				Expect(contents).To(BeEmpty(), "state file should be empty")
			})
		})
	})
})
