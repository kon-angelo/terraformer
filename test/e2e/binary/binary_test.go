// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package binary_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"

	"github.com/gardener/terraformer/pkg/terraformer"
	testutils "github.com/gardener/terraformer/test/utils"
)

const (
	expectedFinalizer = "gardener.cloud/terraformer"
)

var _ = Describe("terraformer", func() {
	var (
		writer                   io.Writer
		pathToTerraformer        string
		terraformerBuildArgsHash string
	)

	buildTerraformer := func(buildArgs ...string) {
		By("building terraformer")
		if pathToTerraformer != "" && testutils.CanReuseLastBuild("terraformer", &terraformerBuildArgsHash, buildArgs) {
			return
		}

		var err error
		pathToTerraformer, err = gexec.Build(
			testutils.TerraformerPackage+"/cmd/terraformer",
			buildArgs...,
		)
		Expect(err).ShouldNot(HaveOccurred())
	}

	BeforeEach(func() {
		writer = GinkgoWriter
	})

	Context("terraform not installed", func() {
		BeforeEach(func() {
			buildTerraformer(
				testutils.LDFlags(testutils.OverwriteTerraformBinary("non-existing"))...,
			)
		})

		It("should fail, if terraform is not installed", func() {
			session, err := gexec.Start(exec.Command(pathToTerraformer), writer, writer)
			Expect(err).NotTo(HaveOccurred())

			Eventually(session).Should(gexec.Exit())
			Expect(session.ExitCode()).NotTo(Equal(0))
			Eventually(session.Err).Should(Say("terraform is not installed"))
		})
	})

	Context("fake terraform installed", func() {
		var (
			baseDir       string
			paths         *terraformer.PathSet
			fakeTerraform testutils.FakeTerraform

			overwriteSleepDuration testutils.Overwrite
			overwriteExitCodes     testutils.Overwrite

			testObjs *testutils.TestObjects
			args     []string
		)

		BeforeEach(func() {
			var err error
			baseDir, err = ioutil.TempDir("", "tf-test-*")
			Expect(err).NotTo(HaveOccurred())
			paths = terraformer.DefaultPaths().WithBaseDir(baseDir)

			var handle testutils.CleanupActionHandle
			handle = testutils.AddCleanupAction(func() {
				defer testutils.RemoveCleanupAction(handle)
				Expect(os.RemoveAll(baseDir)).To(Succeed())
			})
			overwriteSleepDuration = testutils.OverwriteSleepDuration("50ms")
			overwriteExitCodes = testutils.OverwriteExitCode("0")

			buildTerraformer(
				testutils.LDFlags(testutils.OverwriteTerraformBinary(testutils.FakeTerraformBinary))...,
			)

			testObjs = testutils.PrepareTestObjects(ctx, testClient, "", "")
			args = []string{"--zap-devel=true", "--base-dir=" + baseDir}
		})

		JustBeforeEach(func() {
			fakeTerraform = testutils.NewFakeTerraform(
				overwriteSleepDuration,
				overwriteExitCodes,
			)
		})

		Describe("flag validation", func() {
			It("should fail, if no command is given", func() {
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session).Should(gexec.Exit())
				Expect(session.ExitCode()).NotTo(Equal(0))
				Eventually(session.Err).Should(Say("no command was specified"))
			})

			It("should fail, if --configuration-configmap-name is not set", func() {
				args = append(args,
					"apply",
				)
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session).Should(gexec.Exit())
				Expect(session.ExitCode()).NotTo(Equal(0))
				Eventually(session.Err).Should(Say("flag --configuration-configmap-name was not set"))
			})

			It("should fail, if --state-configmap-name is not set", func() {
				args = append(args,
					"apply",
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
				)
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session).Should(gexec.Exit())
				Expect(session.ExitCode()).NotTo(Equal(0))
				Eventually(session.Err).Should(Say("flag --state-configmap-name was not set"))
			})

			It("should fail, if --variables-secret-name is not set", func() {
				args = append(args,
					"apply",
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name="+testObjs.StateConfigMap.Name,
				)
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session).Should(gexec.Exit())
				Expect(session.ExitCode()).NotTo(Equal(0))
				Eventually(session.Err).Should(Say("flag --variables-secret-name was not set"))
			})

			It("should fail, if no kubeconfig has been provided", func() {
				args = append(args,
					"apply",
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name="+testObjs.StateConfigMap.Name,
					"--variables-secret-name="+testObjs.VariablesSecret.Name,
				)
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session).Should(gexec.Exit())
				Expect(session.ExitCode()).NotTo(Equal(0))
				Eventually(session.Err).Should(Say("no configuration has been provided"))
			})
		})

		Describe("config and state fetching", func() {
			It("should correctly fetch config and state from API server", func() {
				args = append(args,
					"apply",
					"--namespace="+testObjs.Namespace,
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name="+testObjs.StateConfigMap.Name,
					"--variables-secret-name="+testObjs.VariablesSecret.Name,
					"--kubeconfig="+kubeconfigFile,
				)
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session).Should(gexec.Exit(0))

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
		})

		Describe("file watcher / state update worker", func() {
			BeforeEach(func() {
				overwriteSleepDuration = testutils.OverwriteSleepDuration("200ms")
			})

			It("should continuously update state ConfigMap on file changes", func() {
				args = append(args,
					"apply",
					"--namespace="+testObjs.Namespace,
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name="+testObjs.StateConfigMap.Name,
					"--variables-secret-name="+testObjs.VariablesSecret.Name,
					"--kubeconfig="+kubeconfigFile,
				)
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session.Err).Should(Say("doing some long running IaaS ops"))

				By("update state file the first time")
				stateContents := "state, generation 1"
				Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

				Eventually(func() map[string]string {
					testObjs.Refresh()
					return testObjs.StateConfigMap.Data
				}, 1, 0.1).Should(HaveKeyWithValue(testutils.StateKey, stateContents))

				By("update state file a second time")
				stateContents = "state, generation 2"
				Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

				Eventually(func() map[string]string {
					testObjs.Refresh()
					return testObjs.StateConfigMap.Data
				}, 2, 0.1).Should(HaveKeyWithValue(testutils.StateKey, stateContents))

				Eventually(session.Err).Should(Say("terraform process finished"))
				Eventually(session.Err).Should(Say("triggering final state update"))
				Eventually(session.Err).Should(Say("successfully stored terraform state"))

				Eventually(session).Should(gexec.Exit(0))
			})
		})

		Describe("signal handling", func() {
			BeforeEach(func() {
				overwriteSleepDuration = testutils.OverwriteSleepDuration("200ms")

				args = append(args,
					"apply",
					"--namespace="+testObjs.Namespace,
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name="+testObjs.StateConfigMap.Name,
					"--variables-secret-name="+testObjs.VariablesSecret.Name,
					"--kubeconfig="+kubeconfigFile,
				)
			})

			It("should relay SIGINT to terraform process", func() {
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session.Err).Should(Say("doing some long running IaaS ops"))

				session.Interrupt()
				Eventually(session.Err).Should(Say(fmt.Sprintf("fake terraform received signal: %s", syscall.SIGINT.String())))
				Eventually(session.Err).Should(Say("terraform process finished successfully"))
				Eventually(session).Should(gexec.Exit(0))
			})

			It("should relay SIGTERM to terraform process", func() {
				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session.Err).Should(Say("doing some long running IaaS ops"))

				session.Terminate()
				Eventually(session.Err).Should(Say(fmt.Sprintf("fake terraform received signal: %s", syscall.SIGINT.String())))
				Eventually(session.Err).Should(Say("terraform process finished successfully"))
				Eventually(session).Should(gexec.Exit(0))
			})
		})

		Describe("finalizer handling", func() {
			BeforeEach(func() {
				overwriteSleepDuration = testutils.OverwriteSleepDuration("200ms")

				args = append(args,
					"--namespace="+testObjs.Namespace,
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name="+testObjs.StateConfigMap.Name,
					"--variables-secret-name="+testObjs.VariablesSecret.Name,
					"--kubeconfig="+kubeconfigFile,
				)
			})

			It("should add finalizer on apply", func() {
				cmd := exec.Command(pathToTerraformer, append(args, "apply")...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session.Err).Should(Say("successfully updated finalizers"))

				Eventually(session.Err).Should(Say("terraform process finished successfully"))
				Eventually(session).Should(gexec.Exit(0))

				testObjs.Refresh()
				Expect(testObjs.ConfigurationConfigMap.Finalizers).To(ContainElement(expectedFinalizer))
				Expect(testObjs.StateConfigMap.Finalizers).To(ContainElement(expectedFinalizer))
				Expect(testObjs.VariablesSecret.Finalizers).To(ContainElement(expectedFinalizer))
			})

			It("should remove finalizer on destroy even when interrupted", func() {
				cmd := exec.Command(pathToTerraformer, append(args, "destroy")...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				// wait for finalizers to be added
				Eventually(session.Err).Should(Say("successfully updated finalizers"))
				testObjs.Refresh()
				Expect(testObjs.ConfigurationConfigMap.Finalizers).To(ContainElement(expectedFinalizer))
				Expect(testObjs.StateConfigMap.Finalizers).To(ContainElement(expectedFinalizer))
				Expect(testObjs.VariablesSecret.Finalizers).To(ContainElement(expectedFinalizer))

				// interrupt during running terraform op
				Eventually(session.Err).Should(Say("doing some long running IaaS ops"))
				session.Interrupt()
				Eventually(session.Err).Should(Say("interrupt received"))

				// wait for terraformer to exit
				Eventually(session.Err).Should(Say("terraform process finished successfully"))
				Eventually(session.Err).Should(Say("successfully updated finalizers"))
				Eventually(session).Should(gexec.Exit(0))

				// expect that finalizers are removed after successful terraform execution
				testObjs.Refresh()
				Expect(testObjs.ConfigurationConfigMap.Finalizers).NotTo(ContainElement(expectedFinalizer))
				Expect(testObjs.StateConfigMap.Finalizers).NotTo(ContainElement(expectedFinalizer))
				Expect(testObjs.VariablesSecret.Finalizers).NotTo(ContainElement(expectedFinalizer))
			})
		})

		Describe("exit code handling", func() {
			runExitCodeTest := func(command string, expectedExitCode int) {
				args = append(args,
					command,
					"--namespace="+testObjs.Namespace,
					"--configuration-configmap-name="+testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name="+testObjs.StateConfigMap.Name,
					"--variables-secret-name="+testObjs.VariablesSecret.Name,
					"--kubeconfig="+kubeconfigFile,
				)

				cmd := exec.Command(pathToTerraformer, args...)
				cmd.Env = fakeTerraform.TerraformerEnv()

				session, err := gexec.Start(cmd, writer, writer)
				Expect(err).NotTo(HaveOccurred())

				Eventually(session.Err).Should(Say("some terraform error"))
				Eventually(session.Err).Should(Say("terraform process finished"))
				Eventually(session.Err).Should(Say("triggering final state update before exiting"))
				Eventually(session.Err).Should(Say("successfully stored terraform state"))

				Eventually(session).Should(gexec.Exit(expectedExitCode))
			}

			Context("terraform commands succeed", func() {
				It("should return exit code 0 if apply succeeds", func() {
					runExitCodeTest("apply", 0)
					Expect(paths.TerminationMessagePath).To(testutils.BeEmptyFile())
				})
				It("should return exit code 0 if destroy succeeds", func() {
					runExitCodeTest("destroy", 0)
					Expect(paths.TerminationMessagePath).To(testutils.BeEmptyFile())
				})
				It("should return exit code 0 if validate and plan succeed", func() {
					runExitCodeTest("validate", 0)
					Expect(paths.TerminationMessagePath).To(testutils.BeEmptyFile())
				})
			})

			Context("init fails", func() {
				BeforeEach(func() {
					overwriteExitCodes = testutils.OverwriteExitCodeForCommands(
						"init", "12",
					)
				})
				It("should return exit code from terraform init", func() {
					runExitCodeTest("apply", 12)
					Expect(paths.TerminationMessagePath).To(testutils.BeFileWithContents(And(
						ContainSubstring("init"),
						ContainSubstring("some terraform error"),
					)), "termination log should contain all terraform logs")
				})
			})

			Context("init succeeds", func() {
				BeforeEach(func() {
					overwriteExitCodes = testutils.OverwriteExitCodeForCommands(
						"init", "0",
						"apply", "42",
						"destroy", "43",
						"validate", "44",
					)
				})

				It("should return exit code from terraform apply", func() {
					runExitCodeTest("apply", 42)
					Expect(paths.TerminationMessagePath).To(testutils.BeFileWithContents(And(
						ContainSubstring("apply"),
						ContainSubstring("doing some long running IaaS ops"),
						ContainSubstring("some terraform error"),
					)), "termination log should contain all terraform logs")
				})
				It("should return exit code from terraform destroy", func() {
					runExitCodeTest("destroy", 43)
					Expect(paths.TerminationMessagePath).To(testutils.BeFileWithContents(And(
						ContainSubstring("destroy"),
						ContainSubstring("doing some long running IaaS ops"),
						ContainSubstring("some terraform error"),
					)), "termination log should contain all terraform logs")
				})
				It("should return exit code from terraform validate", func() {
					runExitCodeTest("validate", 44)
					Expect(paths.TerminationMessagePath).To(testutils.BeFileWithContents(And(
						ContainSubstring("validate"),
						ContainSubstring("doing some long running IaaS ops"),
						ContainSubstring("some terraform error"),
					)), "termination log should contain all terraform logs")
				})

				Context("validate succeeds, but plan fails", func() {
					BeforeEach(func() {
						overwriteExitCodes = testutils.OverwriteExitCodeForCommands(
							"init", "0",
							"validate", "0",
							"plan", "45",
						)
					})
					It("should return exit code from terraform plan", func() {
						runExitCodeTest("validate", 45)
						Expect(paths.TerminationMessagePath).To(testutils.BeFileWithContents(And(
							Not(ContainSubstring("validate")),
							ContainSubstring("plan"),
							ContainSubstring("doing some long running IaaS ops"),
							ContainSubstring("some terraform error"),
						)), "termination log should contain all terraform logs")
					})
				})
			})
		})
	})
})
