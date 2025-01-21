// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer_test

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	mockclient "github.com/gardener/terraformer/pkg/mock/client"
	"github.com/gardener/terraformer/pkg/terraformer"
	testutils "github.com/gardener/terraformer/test/utils"
)

var _ = Describe("Terraformer State", func() {
	var (
		ctrl *gomock.Controller
		// not used by default, only injected in some cases
		c         *mockclient.MockClient
		fakeClock *testing.FakeClock

		tf       *terraformer.Terraformer
		paths    *terraformer.PathSet
		testObjs *testutils.TestObjects

		logBuffer *gbytes.Buffer
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		c = mockclient.NewMockClient(ctrl)
		fakeClock = &testing.FakeClock{}

		baseDir, err := os.MkdirTemp("", "tf-test-*")
		Expect(err).NotTo(HaveOccurred())

		var handle testutils.CleanupActionHandle
		handle = testutils.AddCleanupAction(func() {
			defer testutils.RemoveCleanupAction(handle)
			Expect(os.RemoveAll(baseDir)).To(Succeed())
		})

		paths = terraformer.DefaultPaths().WithBaseDir(baseDir)

		testObjs = testutils.PrepareTestObjects(ctx, testClient, "", "")

		logBuffer = gbytes.NewBuffer()

		tf, err = terraformer.NewTerraformer(
			&terraformer.Config{
				Namespace:                  testObjs.Namespace,
				ConfigurationConfigMapName: testObjs.ConfigurationConfigMap.Name,
				StateConfigMapName:         testObjs.StateConfigMap.Name,
				VariablesSecretName:        testObjs.VariablesSecret.Name,
				RESTConfig:                 restConfig,
			},
			zap.New(zap.UseDevMode(true), zap.WriteTo(io.MultiWriter(GinkgoWriter, logBuffer))),
			paths,
			fakeClock,
		)
		Expect(err).NotTo(HaveOccurred())

		Expect(tf.EnsureTFDirs()).To(Succeed())
	})

	AfterEach(func() {
		ctrl.Finish()
		testutils.RunCleanupActions()
	})

	Describe("#StoreState", func() {
		It("should update state ConfigMap", func() {
			stateContents := "state from new run"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			Expect(tf.StoreState(ctx)).To(Succeed())

			testObjs.Refresh()
			Expect(testObjs.StateConfigMap.Data).To(HaveKeyWithValue(testutils.StateKey, stateContents))
		})
		It("should create new state ConfigMap", func() {
			Expect(testClient.Delete(ctx, testObjs.StateConfigMap)).To(Succeed())

			stateContents := "state from new run"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			Expect(tf.StoreState(ctx)).To(Succeed())

			testObjs.Refresh()
			Expect(testObjs.StateConfigMap.Data).To(HaveKeyWithValue(testutils.StateKey, stateContents))
		})
		It("should fail if state file is not present", func() {
			Expect(os.Remove(paths.StatePath)).To(Or(Succeed(), MatchError(ContainSubstring("no such file"))))

			Expect(tf.StoreState(ctx)).To(MatchError(ContainSubstring("no such file")))
		})
		It("should fail if patch fails", func() {
			tf.InjectClient(c)
			stateContents := "state from new run"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{}), gomock.Any()).Return(fmt.Errorf("fake"))

			Expect(tf.StoreState(ctx)).To(MatchError(ContainSubstring("fake")))
		})
		It("should fail if create fails", func() {
			tf.InjectClient(c)
			stateContents := "state from new run"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{}), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "state"))
			c.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{})).Return(fmt.Errorf("fake"))

			Expect(tf.StoreState(ctx)).To(MatchError(ContainSubstring("fake")))
		})
	})

	Describe("#StartStateUpdateWorker", func() {
		var (
			shutdownWorker func()
		)

		BeforeEach(func() {
			shutdownWorker = tf.StartStateUpdateWorker()
		})

		AfterEach(func() {
			shutdownWorker()
		})

		It("should do nothing if no key is added", func() {
			Consistently(logBuffer).ShouldNot(gbytes.Say("processing work item"))
		})
		It("should update state ConfigMap when key is added", func() {
			stateContents := "state contents"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			By("triggering state update")
			tf.StateUpdateQueue.Add(terraformer.ContinuousStateUpdateKey)
			Eventually(logBuffer).Should(gbytes.Say("processing work item"))

			Eventually(func() map[string]string {
				testObjs.Refresh()
				return testObjs.StateConfigMap.Data
			}, 1, 0.1).Should(HaveKeyWithValue(testutils.StateKey, stateContents))
			Consistently(tf.FinalStateUpdateSucceeded).ShouldNot(Receive(), "should not signal that final state update succeeded")
		})
		It("should signal that final state update succeeded", func() {
			stateContents := "state contents"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			By("triggering final state update")
			tf.StateUpdateQueue.Add(terraformer.FinalStateUpdateKey)
			Eventually(logBuffer).Should(gbytes.Say("processing work item"))

			Eventually(func() map[string]string {
				testObjs.Refresh()
				return testObjs.StateConfigMap.Data
			}, 1, 0.1).Should(HaveKeyWithValue(testutils.StateKey, stateContents))
			Eventually(tf.FinalStateUpdateSucceeded).Should(Receive(), "should signal that final state update succeeded")
		})
		It("should log error if state update fails", func() {
			tf.InjectClient(c)
			stateContents := "state contents"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{}), gomock.Any()).Return(fmt.Errorf("fake"))

			By("triggering state update")
			tf.StateUpdateQueue.Add(terraformer.ContinuousStateUpdateKey)
			Eventually(logBuffer).Should(gbytes.Say("processing work item"))
			Eventually(logBuffer).Should(gbytes.Say("error storing state"))
			Consistently(logBuffer).ShouldNot(gbytes.Say("processing work item"), "should not retry")
			Consistently(tf.FinalStateUpdateSucceeded).ShouldNot(Receive(), "should not signal that final state update succeeded")
		})
		It("should retry if final state update fails", func() {
			tf.InjectClient(c)
			stateContents := "state contents"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			// simulate 5 consecutive failures and success afterwards
			gomock.InOrder(
				c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{}), gomock.Any()).Return(fmt.Errorf("fake")).MaxTimes(5),
				c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{}), gomock.Any()).Return(nil),
			)

			By("triggering final state update")
			tf.StateUpdateQueue.Add(terraformer.FinalStateUpdateKey)
			Eventually(logBuffer).Should(gbytes.Say("processing work item"))
			Eventually(logBuffer).Should(gbytes.Say("error storing state"), "first attempt should fail")
			Eventually(tf.FinalStateUpdateSucceeded).Should(Receive(), "should signal that final state update succeeded")
		})
		It("should gracefully shutdown worker", func() {
			tf.InjectClient(c)
			stateContents := "state contents"
			Expect(ioutil.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			timesUpdatedCounter := 0
			timesUpdatedCh := make(chan int, 2)

			// simulate some long running API requests
			c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{}), gomock.Any()).
				DoAndReturn(func(context.Context, runtime.Object, client.Patch, ...client.PatchOption) error {
					time.Sleep(50 * time.Millisecond)
					timesUpdatedCounter++
					timesUpdatedCh <- timesUpdatedCounter
					return nil
				}).MinTimes(2)

			// add multiple keys
			tf.StateUpdateQueue.Add(terraformer.ContinuousStateUpdateKey)
			tf.StateUpdateQueue.Add(terraformer.FinalStateUpdateTimeout)

			// explicitly call shutdown func here and set to empty func for AfterEach
			shutdownWorker()
			shutdownWorker = func() {}

			Eventually(logBuffer).Should(gbytes.Say("waiting for state-update queue"))
			Eventually(timesUpdatedCh).Should(Receive(Equal(2)))
			fakeClock.Step(5 * time.Second)
		})
	})

	Describe("#StartFileWatcher", func() {
		var (
			shutdownWorker, shutdownFileWatcher func()
		)

		BeforeEach(func() {
			Expect(tf.FetchConfigAndState(ctx)).To(Succeed())

			shutdownWorker = tf.StartStateUpdateWorker()

			var err error
			shutdownFileWatcher, err = tf.StartFileWatcher()
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			shutdownFileWatcher()
			shutdownWorker()
		})

		It("should not update state ConfigMap if file is not changed", func() {
			stateBefore := testObjs.StateConfigMap.DeepCopy()

			Consistently(func() runtime.Object {
				testObjs.Refresh()
				return testObjs.StateConfigMap
			}, 1, 0.1).Should(DeepEqual(stateBefore))
		})
		It("should not update state ConfigMap if file is deleted", func() {
			stateBefore := testObjs.StateConfigMap.DeepCopy()

			Expect(os.Remove(paths.StatePath)).To(Succeed())

			Consistently(func() runtime.Object {
				testObjs.Refresh()
				return testObjs.StateConfigMap
			}, 1, 0.1).Should(DeepEqual(stateBefore))
		})
		It("should update state ConfigMap if file is changed", func() {
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
			}, 1, 0.1).Should(HaveKeyWithValue(testutils.StateKey, stateContents))
		})
	})

	Describe("#TriggerAndWaitForFinalStateUpdate", func() {
		var (
			shutdownWorker func()

			resetVars  func()
			testStdout *gbytes.Buffer
		)

		BeforeEach(func() {
			shutdownWorker = tf.StartStateUpdateWorker()

			testStdout = gbytes.NewBuffer()

			resetVars = test.WithVars(
				&terraformer.Stdout, io.MultiWriter(GinkgoWriter, testStdout),
			)
		})

		AfterEach(func() {
			shutdownWorker()
			resetVars()
		})

		It("should trigger final state update", func(ctx SpecContext) {
			stateContents := "state contents"
			Expect(os.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				wg.Wait()
				ctx.Done()
			}()

			go func() {
				defer GinkgoRecover()
				Expect(tf.TriggerAndWaitForFinalStateUpdate()).To(Succeed())
				wg.Done()
			}()

			Eventually(logBuffer).Should(gbytes.Say("processing work item"))

			Eventually(func() map[string]string {
				testObjs.Refresh()
				return testObjs.StateConfigMap.Data
			}, 1, 0.1).Should(HaveKeyWithValue(testutils.StateKey, stateContents))
			Eventually(logBuffer).Should(gbytes.Say("successfully stored terraform state"))
			wg.Done()
		}, NodeTimeout(time.Second*2))
		It("should retry state update until timeout", func(ctx SpecContext) {
			tf.InjectClient(c)
			stateContents := "state contents"
			Expect(os.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			// always fail patch request, but assert at least 3 retries
			minRetries := 3
			c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&corev1.ConfigMap{}), gomock.Any()).Return(fmt.Errorf("fake")).MinTimes(minRetries)

			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				wg.Wait()
				ctx.Done()
			}()

			go func() {
				defer GinkgoRecover()
				Expect(tf.TriggerAndWaitForFinalStateUpdate()).To(MatchError(ContainSubstring("timed out waiting for final state update")))
				wg.Done()
			}()

			for i := 1; i <= minRetries; i++ {
				Eventually(logBuffer).Should(gbytes.Say("processing work item"), fmt.Sprintf("%d. attempt", i))
			}
			fakeClock.Step(terraformer.FinalStateUpdateTimeout)
			Eventually(logBuffer).Should(gbytes.Say("error updating state"))
			wg.Done()
			Eventually(testStdout).Should(gbytes.Say(stateContents), "should copy state contents to stdout")
		}, NodeTimeout(time.Second*2))
	})

	Describe("#LogStateContentsToStdout", func() {
		var (
			resetVars  func()
			testStdout *gbytes.Buffer
		)

		BeforeEach(func() {
			testStdout = gbytes.NewBuffer()

			resetVars = test.WithVars(
				&terraformer.Stdout, io.MultiWriter(GinkgoWriter, testStdout),
			)
		})

		AfterEach(func() {
			resetVars()
		})

		It("should copy state contents to stdout", func() {
			stateContents := `
state contents
spanning multiple lines
`
			Expect(os.WriteFile(paths.StatePath, []byte(stateContents), 0644)).To(Succeed())

			Expect(tf.LogStateContentsToStdout()).To(Succeed())
			Eventually(testStdout).Should(gbytes.Say(stateContents), "should copy state contents to stdout")
		})
	})
})
