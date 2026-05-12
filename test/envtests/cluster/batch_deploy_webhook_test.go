/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
)

var _ = Describe("RollingUpdateBatchSize deploy validation", func() {
	const (
		clusterName = "batch-deploy-webhook-cluster"
	)

	ctx := context.TODO()

	clusterNamespacedName := uniqueNamespacedName(clusterName)

	AfterEach(func() {
		deleteCluster(ctx, clusterNamespacedName)
	})

	Context("Deploy validation", func() {
		Context("spec.rackConfig with rollingUpdateBatchSize (rack namespaces)", func() {
			Context("negative", func() {
				It("rejects create when replication-factor is 1 by using RollingUpdateBatchSizePercent", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 1)
					aeroCluster.Spec.RackConfig.Racks = testCluster.GetDummyRackConf(1, 2)
					aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
					aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = ptr.To(intstr.FromString("100%"))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"spec.rackConfig.rollingUpdateBatchSize",
							"`test`",
							"replication-factor 1",
						).
						Validate(err)
				})

				It("rejects create when replication-factor is 1 by using RollingUpdateBatchSize", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 1)
					aeroCluster.Spec.RackConfig.Racks = testCluster.GetDummyRackConf(1, 2)
					aeroCluster.Spec.RackConfig.Namespaces = []string{"test"}
					aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = ptr.To(intstr.FromInt32(10))

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"spec.rackConfig.rollingUpdateBatchSize",
							"`test`",
							"replication-factor 1",
						).
						Validate(err)
				})

				It("rejects create when a rack-enabled namespace is configured on a single rack only", func() {
					aeroCluster := testCluster.CreateDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 2)
					aeroCluster.Spec.RackConfig.Racks = testCluster.GetDummyRackConf(1, 2)
					aeroCluster.Spec.RackConfig.Namespaces = []string{"test", "bar"}
					aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = ptr.To(intstr.FromString("100%"))
					aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &asdbv1.AerospikeConfigSpec{
						Value: map[string]interface{}{
							"namespaces": []interface{}{
								map[string]interface{}{
									"name":               "bar",
									"replication-factor": 2,
									"storage-engine": map[string]interface{}{
										"type":      "memory",
										"data-size": 1073741824,
									},
								},
							},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"spec.rackConfig.rollingUpdateBatchSize",
							"`bar`",
							"only one rack",
						).
						Validate(err)
				})
			})

			Context("positive", func() {
				It("allows create when the namespace is configured on every rack", func() {
					barNS := &asdbv1.AerospikeConfigSpec{
						Value: map[string]interface{}{
							"namespaces": []interface{}{
								map[string]interface{}{
									"name":               "bar",
									"replication-factor": 2,
									"storage-engine": map[string]interface{}{
										"type":      "memory",
										"data-size": 1073741824,
									},
								},
							},
						},
					}

					aeroCluster := testCluster.CreateDummyAerospikeClusterWithRF(clusterNamespacedName, 2, 2)
					aeroCluster.Spec.RackConfig.Racks = testCluster.GetDummyRackConf(1, 2)
					aeroCluster.Spec.RackConfig.Namespaces = []string{"test", "bar"}
					aeroCluster.Spec.RackConfig.RollingUpdateBatchSize = ptr.To(intstr.FromString("100%"))
					aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = barNS
					aeroCluster.Spec.RackConfig.Racks[1].InputAerospikeConfig = barNS

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).ToNot(HaveOccurred())
				})
			})
		})
	})
})
