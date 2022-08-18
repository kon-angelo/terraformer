// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package binary_test

import (
	"context"
	"fmt"
	"time"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/pkg/version"
	testutils "github.com/gardener/terraformer/test/utils"
)

const (
	prefix          = "terraformer-it-"
	terraformerName = "terraformer"

	cloudProviderSecretName = "cloudprovider"
	keyAccessKeyID          = "accessKeyID"
	keySecretAccessKey      = "secretAccessKey"

	terraformerImage = "eu.gcr.io/gardener-project/gardener/terraformer-aws"
)

var (
	log      logr.Logger
	testObjs *testutils.TestObjects

	terraformerImageTag string
)

var _ = Describe("Pod E2E test", func() {
	BeforeEach(func() {
		terraformerImageTag = version.Get().GitVersion

		By("creating test objects")
		testObjs = testutils.PrepareTestObjects(ctx, testClient, prefix, "")

		log = runtimelog.Log.WithValues("namespace", testObjs.Namespace)
		log.Info("using namespace")
	})

	It("should apply and destroy config successfully", func() {
		var keyPairName string

		testutils.AddCleanupAction(func() {
			// ensure, that we don't leak any resources even if terraformer destroy fails,
			// so we don't have to cleanup manually
			By("ensuring cleanup of AWS resources")
			_, err := ec2Client.DeleteKeyPairWithContext(ctx, &ec2.DeleteKeyPairInput{
				KeyName: awssdk.String(testObjs.Namespace + "-ssh-publickey"),
			})
			if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "InvalidKeyPair.NotFound" {
				return
			}
			Expect(err).NotTo(HaveOccurred())
		})

		defer func() {
			By("deploying terraformer destroy pod")
			pod, err := deployTerraformerPod(ctx, "destroy")
			Expect(err).NotTo(HaveOccurred())

			exitCode, terminationMessage, err := waitForPod(ctx, pod, podTimeout)
			Expect(err).NotTo(HaveOccurred())
			Expect(exitCode).To(BeEquivalentTo(0))
			Expect(terminationMessage).To(BeEmpty())

			By("verifying resource deletion on AWS")
			verifyDeletionAWS(ctx, keyPairName)

			By("verifying deletion in state ConfigMap")
			verifyStateConfigMapDeletion()
		}()

		By("deploying cloudprovider secret into namespace")
		Expect(deployCloudProviderSecret(ctx, *accessKeyID, *secretAccessKey)).To(Succeed())

		By("deploying terraformer config into namespace")
		Expect(deployTerraformConfig(ctx)).To(Succeed())

		By("deploying terraformer apply pod")
		pod, err := deployTerraformerPod(ctx, "apply")
		Expect(err).NotTo(HaveOccurred())

		exitCode, terminationMessage, err := waitForPod(ctx, pod, podTimeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(exitCode).To(BeEquivalentTo(0))
		Expect(terminationMessage).To(BeEmpty())

		By("verifying resource creation on AWS")
		keyPairName = verifyCreationAWS(ctx)

		By("verifying creation in state ConfigMap")
		verifyStateConfigMapCreation()
	})

	It("should fail because of invalid credentials", func() {
		const fakeSecretAccessKey = "invalid"

		By("deploying invalid cloudprovider secret into namespace")
		Expect(deployCloudProviderSecret(ctx, *accessKeyID, fakeSecretAccessKey)).To(Succeed())

		By("deploying terraformer config into namespace")
		Expect(deployTerraformConfig(ctx)).To(Succeed())

		By("deploying terraformer apply pod")
		pod, err := deployTerraformerPod(ctx, "apply")
		Expect(err).NotTo(HaveOccurred())

		exitCode, terminationMessage, err := waitForPod(ctx, pod, podTimeout)
		Expect(err).NotTo(HaveOccurred())
		Expect(exitCode).To(BeEquivalentTo(1))
		Expect(terminationMessage).To(ContainSubstring("error validating provider credentials"))
	})
})

func deployCloudProviderSecret(ctx context.Context, accessKeyID, secretAccessKey string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cloudProviderSecretName,
			Namespace: testObjs.Namespace,
		},
		Data: map[string][]byte{
			keyAccessKeyID:     []byte(accessKeyID),
			keySecretAccessKey: []byte(secretAccessKey),
		},
	}
	return testClient.Create(ctx, secret)
}

func deployTerraformConfig(ctx context.Context) error {
	const sshPublicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDcSZKq0lM9w+ElLp9I9jFvqEFbOV1+iOBX7WEe66GvPLOWl9ul03ecjhOf06+FhPsWFac1yaxo2xj+SJ+FVZ3DdSn4fjTpS9NGyQVPInSZveetRw0TV0rbYCFBTJuVqUFu6yPEgdcWq8dlUjLqnRNwlelHRcJeBfACBZDLNSxjj0oUz7ANRNCEne1ecySwuJUAz3IlNLPXFexRT0alV7Nl9hmJke3dD73nbeGbQtwvtu8GNFEoO4Eu3xOCKsLw6ILLo4FBiFcYQOZqvYZgCb4ncKM52bnABagG54upgBMZBRzOJvWp0ol+jK3Em7Vb6ufDTTVNiQY78U6BAlNZ8Xg+LUVeyk1C6vWjzAQf02eRvMdfnRCFvmwUpzbHWaVMsQm8gf3AgnTUuDR0ev1nQH/5892wZA86uLYW/wLiiSbvQsqtY1jSn9BAGFGdhXgWLAkGsd/E1vOT+vDcor6/6KjHBm0rG697A3TDBRkbXQ/1oFxcM9m17RteCaXuTiAYWMqGKDoJvTMDc4L+Uvy544pEfbOH39zfkIYE76WLAFPFsUWX6lXFjQrX3O7vEV73bCHoJnwzaNd03PSdJOw+LCzrTmxVezwli3F9wUDiBRB0HkQxIXQmncc1HSecCKALkogIK+1e1OumoWh6gPdkF4PlTMUxRitrwPWSaiUIlPfCpQ=="

	testObjs.ConfigurationConfigMap.Data[testutils.ConfigMainKey] = `provider "aws" {
  access_key = var.ACCESS_KEY_ID
  secret_key = var.SECRET_ACCESS_KEY
  region     = "` + *region + `"
}

resource "aws_key_pair" "keypair" {
  key_name   = "` + testObjs.Namespace + `-ssh-publickey"
  public_key = "` + sshPublicKey + `"
}
`
	// append to test config instead of replacing it (already contains tf var declaration `SOME_VAR`),
	// passing values for undeclared tf var is deprecated and will become an error in a terraform future release.
	testObjs.ConfigurationConfigMap.Data[testutils.ConfigVarsKey] += `
variable "ACCESS_KEY_ID" {
  description = "AWS Access Key ID of technical user"
  type        = string
}

variable "SECRET_ACCESS_KEY" {
  description = "AWS Secret Access Key of technical user"
  type        = string
}
`

	if err := testClient.Patch(ctx, testObjs.ConfigurationConfigMap, client.Merge); err != nil {
		return err
	}

	// PrepareTestObjects creates a state ConfigMap by default -> delete it
	return testClient.Delete(ctx, testObjs.StateConfigMap)
}

func deployTerraformerPod(ctx context.Context, command string) (*corev1.Pod, error) {
	if err := createOrUpdateTerraformerAuth(ctx); err != nil {
		return nil, err
	}

	generateName := fmt.Sprintf("%stf-%s-", prefix, command)
	log.Info("deploying terraformer pod", "generateName", generateName, "command", command)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
			Namespace:    testObjs.Namespace,
			Labels: map[string]string{
				"origin":  "test-machinery",
				"test":    "terraformer-pod-e2e",
				"purpose": "testing",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:            "terraformer",
				Image:           terraformerImage + ":" + terraformerImageTag,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{
					"/terraformer",
					command,
					"--zap-log-level=debug",
					"--configuration-configmap-name=" + testObjs.ConfigurationConfigMap.Name,
					"--state-configmap-name=" + testObjs.StateConfigMap.Name,
					"--variables-secret-name=" + testObjs.VariablesSecret.Name,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1.5Gi"),
					},
				},
				Env: []corev1.EnvVar{{
					Name: "TF_VAR_ACCESS_KEY_ID",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: cloudProviderSecretName,
							},
							Key: keyAccessKeyID,
						},
					},
				}, {
					Name: "TF_VAR_SECRET_ACCESS_KEY",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: cloudProviderSecretName,
							},
							Key: keySecretAccessKey,
						},
					},
				}},
				TerminationMessagePath: "/terraform-termination-log",
			}},
			RestartPolicy:                 corev1.RestartPolicyNever,
			ServiceAccountName:            terraformerName,
			TerminationGracePeriodSeconds: pointer.Int64Ptr(60),
		},
	}

	if err := testClient.Create(ctx, pod); err != nil {
		return nil, err
	}
	log.Info("deployed terraformer pod", "pod", testutils.ObjectKeyFromObject(pod))
	return pod, nil
}

func waitForPod(ctx context.Context, pod *corev1.Pod, timeout time.Duration) (exitCode int32, terminationMessage string, err error) {
	exitCode = int32(-1)
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	podLog := log.WithValues("pod", testutils.ObjectKeyFromObject(pod))

	err = wait.PollUntil(5*time.Second, func() (done bool, err error) {
		podLog.Info("waiting for terraformer pod to be completed...")
		err = testClient.Get(pollCtx, testutils.ObjectKeyFromObject(pod), pod)
		if apierrors.IsNotFound(err) {
			podLog.Info("terraformer pod disappeared unexpectedly, somebody must have manually deleted it")
			return true, nil
		}
		if err != nil {
			podLog.Error(err, "error retrieving pod")
			return true, err
		}

		// Check whether the Pod has been successful
		var (
			phase             = pod.Status.Phase
			containerStatuses = pod.Status.ContainerStatuses
		)

		if (phase == corev1.PodSucceeded || phase == corev1.PodFailed) && len(containerStatuses) > 0 {
			if containerStateTerminated := containerStatuses[0].State.Terminated; containerStateTerminated != nil {
				exitCode = containerStateTerminated.ExitCode
				terminationMessage = containerStateTerminated.Message
			}
			return true, nil
		}

		podLog.Info("waiting for terraformer pod to be completed, pod hasn't finished yet", "phase", phase, "len-of-containerstatuses", len(containerStatuses))
		return false, nil
	}, pollCtx.Done())
	if err != nil {
		err = fmt.Errorf("error waiting for terraformer pod to be completed: %w", err)
		return
	}

	podLog.Info("terraformer pod terminated", "exitCode", exitCode, "terminationMessage", terminationMessage)
	return
}

func createOrUpdateTerraformerAuth(ctx context.Context) error {
	if err := createOrUpdateServiceAccount(ctx); err != nil {
		return err
	}
	if err := createOrUpdateRole(ctx); err != nil {
		return err
	}
	return createOrUpdateRoleBinding(ctx)
}

func createOrUpdateServiceAccount(ctx context.Context) error {
	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: testObjs.Namespace, Name: terraformerName}}
	_, err := controllerutil.CreateOrUpdate(ctx, testClient, serviceAccount, func() error {
		return nil
	})
	return err
}

func createOrUpdateRole(ctx context.Context) error {
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: testObjs.Namespace, Name: terraformerName}}
	_, err := controllerutil.CreateOrUpdate(ctx, testClient, role, func() error {
		role.Rules = []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"configmaps", "secrets"},
			Verbs:     []string{"*"},
		}}
		return nil
	})
	return err
}

func createOrUpdateRoleBinding(ctx context.Context) error {
	roleBinding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Namespace: testObjs.Namespace, Name: terraformerName}}
	_, err := controllerutil.CreateOrUpdate(ctx, testClient, roleBinding, func() error {
		roleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     terraformerName,
		}
		roleBinding.Subjects = []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      terraformerName,
			Namespace: testObjs.Namespace,
		}}
		return nil
	})
	return err
}

func verifyCreationAWS(ctx context.Context) string {
	const sshPublicKeyDigest = "46:ca:46:0e:8e:1d:bc:0c:45:31:ee:0f:43:5f:9b:f1"

	describeKeyPairsOutput, err := ec2Client.DescribeKeyPairsWithContext(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []*string{awssdk.String(testObjs.Namespace + "-ssh-publickey")},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(describeKeyPairsOutput.KeyPairs[0].KeyFingerprint).To(gstruct.PointTo(Equal(sshPublicKeyDigest)))
	return *describeKeyPairsOutput.KeyPairs[0].KeyName
}

func verifyStateConfigMapCreation() {
	Eventually(func() map[string]string {
		testObjs.Refresh()
		return testObjs.StateConfigMap.Data
	}, 5, 0.1).Should(HaveKeyWithValue(testutils.StateKey, ContainSubstring(testObjs.Namespace+"-ssh-publickey")))
}

func verifyDeletionAWS(ctx context.Context, keyPairName string) {
	if keyPairName != "" {
		describeKeyPairsOutput, err := ec2Client.DescribeKeyPairsWithContext(ctx, &ec2.DescribeKeyPairsInput{KeyNames: []*string{awssdk.String(keyPairName)}})
		Expect(err).To(HaveOccurred())
		awsErr, _ := err.(awserr.Error)
		Expect(awsErr.Code()).To(Equal("InvalidKeyPair.NotFound"))
		Expect(describeKeyPairsOutput.KeyPairs).To(BeEmpty())
	}
}

func verifyStateConfigMapDeletion() {
	Eventually(func() map[string]string {
		testObjs.Refresh()
		return testObjs.StateConfigMap.Data
	}, 5, 0.1).Should(HaveKeyWithValue(testutils.StateKey, Not(ContainSubstring(testObjs.Namespace+"-ssh-publickey"))))
}
