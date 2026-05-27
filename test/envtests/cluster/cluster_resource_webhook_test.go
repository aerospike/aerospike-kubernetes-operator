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

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
)

var _ = Describe("Cluster resource validation", func() {
	const (
		clusterName = "pod-resource-webhook-cluster"
	)

	ctx := context.TODO()

	clusterNamespacedName := uniqueNamespacedName(clusterName)

	AfterEach(func() {
		deleteCluster(ctx, clusterNamespacedName)
	})

	Context("Deploy validation", func() {
		Context("spec.podSpec.aerospikeContainerSpec.resources", func() {
			Context("negative", func() {
				It("rejects create when resource limits are less than requests", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources = podResourcesWithLimitsLessThanRequests()

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					// err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"resources.Limits cannot be less than resource.Requests",
						).
						Validate(err)
				})
			})
		})

		Context("spec.podSpec.aerospikeInitContainerSpec.resources", func() {
			Context("negative", func() {
				It("rejects create when resource limits are less than requests", func() {
					aeroCluster := testCluster.CreateDummyAerospikeCluster(clusterNamespacedName, 2)
					if aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec == nil {
						aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec = &asdbv1.AerospikeInitContainerSpec{}
					}

					aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources = podResourcesWithLimitsLessThanRequests()

					// err := testCluster.DeployCluster(envtests.K8sClient, ctx, aeroCluster)
					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())

					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"vaerospikecluster.kb.io\"",
							"resources.Limits cannot be less than resource.Requests",
						).
						Validate(err)
				})
			})
		})
	})
})
