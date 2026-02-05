package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test"
)

var _ = Describe(
	"SecurityContext", func() {

		ctx := goctx.TODO()

		// Context("Check for podSpec securityContext", func() {
		// 	securityContextTest(ctx, true, false, false)
		// })
		Context(
			"Check for aerospike-server container securityContext", func() {
				securityContextTest(ctx, false, true, false)
			},
		)
		Context(
			"Check for aerospike-init securityContext", func() {
				securityContextTest(ctx, false, false, true)
			},
		)
	},
)

func securityContextTest(
	ctx goctx.Context, checkPodSpec bool, checkAeroServer bool,
	checkAeroInit bool,
) {
	clusterName := fmt.Sprintf("security-context-%d", GinkgoParallelProcess())
	clusterNamespacedName := test.GetNamespacedName(
		clusterName, namespace,
	)

	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedName.Name,
					Namespace: clusterNamespacedName.Namespace,
				},
			}
			Expect(DeleteCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())
			Expect(CleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)).ToNot(HaveOccurred())
		},
	)
	It(
		"Validate SecurityContext applied", func() {
			By("DeployCluster with SecurityContext")

			aeroCluster := createDummyAerospikeCluster(
				clusterNamespacedName, 2,
			)

			if checkPodSpec {
				aeroCluster.Spec.PodSpec.SecurityContext = &corev1.PodSecurityContext{
					SupplementalGroups: []int64{1000},
				}
			}

			if checkAeroServer {
				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext = &corev1.SecurityContext{Privileged: new(bool)}
			}

			if checkAeroInit {
				aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.SecurityContext = &corev1.SecurityContext{Privileged: new(bool)}
			}

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Validate")
			validateSecurityContext(
				aeroCluster,
			)
		},
	)

	It(
		"Validate SecurityContext updated", func() {
			By("DeployCluster")

			aeroCluster := createDummyAerospikeCluster(
				clusterNamespacedName, 2,
			)

			Expect(DeployCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("UpdateCluster with SecurityContext")

			if checkPodSpec {
				aeroCluster.Spec.PodSpec.SecurityContext = &corev1.PodSecurityContext{
					SupplementalGroups: []int64{1000},
				}
			}

			if checkAeroServer {
				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext = &corev1.SecurityContext{Privileged: new(bool)}
			}

			if checkAeroInit {
				aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.SecurityContext = &corev1.SecurityContext{Privileged: new(bool)}
			}

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Validate")
			validateSecurityContext(
				aeroCluster,
			)

			if checkPodSpec {
				aeroCluster.Spec.PodSpec.SecurityContext = nil
			}

			if checkAeroServer {
				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext = nil
			}

			if checkAeroInit {
				aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.SecurityContext = nil
			}

			Expect(updateCluster(k8sClient, ctx, aeroCluster)).ToNot(HaveOccurred())

			By("Validate")
			validateSecurityContext(
				aeroCluster,
			)
		},
	)
}

func validateSecurityContext(
	aeroCluster *asdbv1.AerospikeCluster,
) {
	pods, err := getClusterPodList(
		k8sClient, goctx.TODO(),
		aeroCluster,
	)
	Expect(err).ToNot(HaveOccurred())

	Expect(pods.Items).ToNot(BeEmpty())

	for podIndex := range pods.Items {
		// TODO: get pod.Spec container by name.
		// var podSpecSecCtx corev1.PodSecurityContext
		// var serverSecCtx corev1.SecurityContext
		// var initSecCtx corev1.SecurityContext
		if aeroCluster.Spec.PodSpec.SecurityContext != nil {
			Expect(pods.Items[podIndex].Spec.SecurityContext).To(Equal(aeroCluster.Spec.PodSpec.SecurityContext))
		}

		if aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext != nil {
			Expect(pods.Items[podIndex].Spec.Containers[0].SecurityContext).To(Equal(
				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext,
			),
			)
		}

		if aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec != nil &&
			aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.SecurityContext != nil {
			// Find aerospike-init container by name
			aerospikeInitContainer := test.GetContainerByName(
				pods.Items[podIndex].Spec.InitContainers,
				asdbv1.AerospikeInitContainerName,
			)
			Expect(aerospikeInitContainer).NotTo(BeNil(), "aerospike-init container not found")
			Expect(aerospikeInitContainer.SecurityContext).To(Equal(
				aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.SecurityContext,
			),
			)
		}
	}
}
