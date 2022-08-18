// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package binary_test

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	testutils "github.com/gardener/terraformer/test/utils"
)

// This test suite runs e2e tests against a terraformer Pod. It uses an existing cluster (given by the KUBECONFIG
// env var) and deploys a terraformer `apply` Pod, that will create some lightweight resource (ec2 keypair) on AWS.
// The test validates, that the resource was created on AWS using the AWS go-sdk and that the state ConfigMap has
// been updated accordingly. After that, the test deploys a terraformer `destroy` Pod and validates again the changes
// on AWS and the state ConfigMap.
func TestTerraformer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Terraformer Pod E2E Suite")
}

const (
	suiteTimeout = 15 * time.Minute
	podTimeout   = 5 * time.Minute
)

var (
	ctx        context.Context
	ctxCancel  context.CancelFunc
	testEnv    *envtest.Environment
	restConfig *rest.Config
	testClient client.Client

	accessKeyID     = flag.String("access-key-id", "", "AWS access key id")
	secretAccessKey = flag.String("secret-access-key", "", "AWS secret access key")
	region          = flag.String("region", "", "AWS region")
	ec2Client       ec2iface.EC2API
)

var _ = BeforeSuite(func() {
	ctx, ctxCancel = context.WithTimeout(context.Background(), suiteTimeout)

	runtimelog.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	By("starting test environment")
	testEnv = &envtest.Environment{
		UseExistingCluster: pointer.BoolPtr(true),
	}

	var err error
	restConfig, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(restConfig).ToNot(BeNil())

	testClient, err = client.New(restConfig, client.Options{})
	Expect(err).ToNot(HaveOccurred())

	By("creating AWS client")
	flag.Parse()
	Expect(validateFlags()).To(Succeed())

	ec2Client, err = newEC2Client(*accessKeyID, *secretAccessKey, *region)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("running cleanup actions")
	testutils.RunCleanupActions()

	By("stopping test environment")
	Expect(testEnv.Stop()).To(Succeed())

	ctxCancel()
})

func validateFlags() error {
	if len(*accessKeyID) == 0 {
		return fmt.Errorf("need an AWS access key id")
	}
	if len(*secretAccessKey) == 0 {
		return fmt.Errorf("need an AWS secret access key")
	}
	if len(*region) == 0 {
		return fmt.Errorf("need an AWS region")
	}

	return nil
}

func newEC2Client(accessKeyID, secretAccessKey, region string) (ec2iface.EC2API, error) {
	credentialsConfig := &aws.Config{
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretAccessKey, ""),
	}
	regionConfig := &aws.Config{Region: aws.String(region)}

	s, err := session.NewSession(credentialsConfig)
	if err != nil {
		return nil, err
	}

	return ec2.New(s, regionConfig), nil
}
