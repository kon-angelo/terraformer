// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/gardener/terraformer/test/utils"
)

func TestTerraformer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Terraformer Suite")
}

var (
	ctx        context.Context
	testEnv    *envtest.Environment
	restConfig *rest.Config
	testClient client.Client
)

var _ = BeforeSuite(func() {
	ctx = context.Background()
	runtimelog.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	By("starting test environment")
	testEnv = &envtest.Environment{}

	var err error
	restConfig, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(restConfig).ToNot(BeNil())

	testClient, err = client.New(restConfig, client.Options{})
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("running cleanup actions")
	utils.RunCleanupActions()
	gexec.CleanupBuildArtifacts()

	By("stopping test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
