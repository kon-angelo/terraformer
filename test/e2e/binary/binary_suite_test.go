// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package binary_test

import (
	"context"
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	testutils "github.com/gardener/terraformer/test/utils"
)

// This test suite runs e2e tests against the terraformer binary. It builds the binary using gexec.Build and optionally
// builds a terraformer fake binary to mock terraform executions in terraformer.
// It starts a fake control plane (via envtest) and doesn't require an existing cluster by default, so it can be run in
// normal CI pipelines.
// Nevertheless, an existing cluster can still be used like in the unit tests by setting the KUBECONFIG env var and
// setting USE_EXISTING_CLUSTER=true.
func TestTerraformer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Terraformer Binary E2E Suite")
}

var (
	ctx            context.Context
	testEnv        *envtest.Environment
	restConfig     *rest.Config
	kubeconfigFile string
	testClient     client.Client
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
	kubeconfigFile = writeKubeconfigToFile(restConfig)

	var handle testutils.CleanupActionHandle
	handle = testutils.AddCleanupAction(func() {
		defer testutils.RemoveCleanupAction(handle)
		Expect(os.Remove(kubeconfigFile)).To(Succeed())
	})

	testClient, err = client.New(restConfig, client.Options{})
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("running cleanup actions")
	testutils.RunCleanupActions()
	gexec.CleanupBuildArtifacts()

	By("stopping test environment")
	Expect(testEnv.Stop()).To(Succeed())
})

func writeKubeconfigToFile(config *rest.Config) string {
	file, err := ioutil.TempFile("", "tf-test-kubeconfig-*.yaml")
	Expect(err).NotTo(HaveOccurred())

	Expect(clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"terraformer": {
			Server:                   config.Host,
			InsecureSkipTLSVerify:    config.Insecure,
			CertificateAuthority:     config.CAFile,
			CertificateAuthorityData: config.CAData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"terraformer": {
			ClientCertificate:     config.TLSClientConfig.CertFile,
			ClientCertificateData: config.TLSClientConfig.CertData,
			ClientKey:             config.TLSClientConfig.KeyFile,
			ClientKeyData:         config.TLSClientConfig.KeyData,
			Token:                 config.BearerToken,
			TokenFile:             config.BearerTokenFile,
			Username:              config.Username,
			Password:              config.Password,
		}},
		Contexts: map[string]*clientcmdapi.Context{"terraformer": {
			Cluster:  "terraformer",
			AuthInfo: "terraformer",
		}},
		CurrentContext: "terraformer",
	}, file.Name())).To(Succeed())

	return file.Name()
}
