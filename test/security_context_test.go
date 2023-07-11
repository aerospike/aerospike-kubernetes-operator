package test

import (
	goctx "context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
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
	It(
		"Validate SecurityContext applied", func() {
			By("DeployCluster with SecurityContext")
			clusterNamespacedName := getNamespacedName(
				"security-context-create", namespace,
			)
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

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Validate")
			validateSecurityContext(
				aeroCluster,
			)

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
		},
	)

	It(
		"Validate SecurityContext updated", func() {
			By("DeployCluster")
			clusterNamespacedName := getNamespacedName(
				"security-context-updated", namespace,
			)
			aeroCluster := createDummyAerospikeCluster(
				clusterNamespacedName, 2,
			)

			err := deployCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())

			By("UpdateCluster with SecurityContext")
			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
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

			aeroCluster, err = getCluster(k8sClient, ctx, clusterNamespacedName)
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

			err = deleteCluster(k8sClient, ctx, aeroCluster)
			Expect(err).ToNot(HaveOccurred())
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

	Expect(len(pods.Items)).ToNot(BeZero())

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
