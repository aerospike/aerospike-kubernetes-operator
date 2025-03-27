package cluster

import (
	goctx "context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/test"
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
	clusterNameCreate := fmt.Sprintf("security-context-create-%d", GinkgoParallelProcess())
	clusterNamespacedNameCreate := test.GetNamespacedName(
		clusterNameCreate, namespace,
	)

	clusterNameUpdate := fmt.Sprintf("security-context-update-%d", GinkgoParallelProcess())
	clusterNamespacedNameUpdate := test.GetNamespacedName(
		clusterNameUpdate, namespace,
	)

	AfterEach(
		func() {
			aeroCluster := &asdbv1.AerospikeCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterNamespacedNameCreate.Name,
					Namespace: clusterNamespacedNameCreate.Namespace,
				},
			}
			_ = deleteCluster(k8sClient, ctx, aeroCluster)
			_ = cleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)

			aeroCluster.Name = clusterNamespacedNameUpdate.Name
			aeroCluster.Namespace = clusterNamespacedNameUpdate.Namespace

			_ = deleteCluster(k8sClient, ctx, aeroCluster)
			_ = cleanupPVC(k8sClient, aeroCluster.Namespace, aeroCluster.Name)
		},
	)
	It(
		"Validate SecurityContext applied", func() {
			By("DeployCluster with SecurityContext")

			aeroCluster := createDummyAerospikeCluster(
				clusterNamespacedNameCreate, 2,
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

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

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
				clusterNamespacedNameUpdate, 2,
			)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("UpdateCluster with SecurityContext")

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedNameUpdate)
			Expect(err).ToNot(HaveOccurred())

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

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Validate")
			validateSecurityContext(
				aeroCluster,
			)

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedNameUpdate)
			Expect(err).ToNot(HaveOccurred())

			if checkPodSpec {
				aeroCluster.Spec.PodSpec.SecurityContext = nil
			}

			if checkAeroServer {
				aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext = nil
			}

			if checkAeroInit {
				aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.SecurityContext = nil
			}

			err = updateCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

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
			Expect(pods.Items[podIndex].Spec.InitContainers[0].SecurityContext).To(Equal(
				aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.SecurityContext,
			),
			)
		}
	}
}
