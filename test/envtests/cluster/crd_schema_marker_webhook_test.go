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

// CRD OpenAPI / kubebuilder marker coverage for the v1 AerospikeCluster storage schema
// (see config/crd/bases/asdb.aerospike.com_aerospikeclusters.yaml). Some markers exist only on
// older CRD versions in the same manifest (e.g. operations maxItems); those are not tested here.
// This file also holds related admission tests (allowed enum combinations, mutating defaults such
// as implicit rack id) that are run in the same envtest suite.

package cluster

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	testCluster "github.com/aerospike/aerospike-kubernetes-operator/v4/test/cluster"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/envtests"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/test/testutil"
)

var _ = Describe("CRD schema marker validation", func() {
	ctx := context.TODO()

	var nsName types.NamespacedName

	BeforeEach(func() {
		nsName = types.NamespacedName{}
	})

	AfterEach(func() {
		if nsName.Name != "" {
			deleteCluster(ctx, nsName)
		}
	})

	Context("Deploy validation", func() {
		Context("spec.operations", func() {
			Context("negative", func() {
				It("rejects invalid operation kind (Enum)", func() {
					nsName = uniqueNamespacedName("crd-ops-kind")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationKind("NotAValidKind"), ID: "x"},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "spec.operations", "kind").
						Validate(err)
				})

				It("rejects empty operation id (MinLength=1)", func() {
					nsName = uniqueNamespacedName("crd-ops-id-empty")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationWarmRestart, ID: ""},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "spec.operations", "id").
						Validate(err)
				})

				It("rejects operation id longer than 20 characters (MaxLength=20)", func() {
					nsName = uniqueNamespacedName("crd-ops-id-long")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.Operations = []asdbv1.OperationSpec{
						{Kind: asdbv1.OperationWarmRestart, ID: strings.Repeat("a", 21)},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "spec.operations", "id").
						Validate(err)
				})
			})
		})

		Context("spec.rackConfig.racks[].id", func() {
			Context("negative", func() {
				It("rejects rack id above 1000000 (Maximum)", func() {
					nsName = uniqueNamespacedName("crd-rack-id-max")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					st := getStorageSpecForDevice("/test/dev/xvdf")
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1000001, Revision: "v1", InputStorage: &st},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "id").
						Validate(err)
				})
			})
		})

		Context("spec.rackConfig.rack id semantics", func() {
			Context("positive", func() {
				It("adds implicit default rack when racks are omitted (DefaultRackID)", func() {
					nsName = uniqueNamespacedName("crd-rack-id-default-implicit")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					Expect(aeroCluster.Spec.RackConfig.Racks).To(BeEmpty())

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					fetched, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())
					Expect(fetched.Spec.RackConfig.Racks).To(HaveLen(1))
					Expect(fetched.Spec.RackConfig.Racks[0].ID).To(Equal(asdbv1.DefaultRackID))
				})

				It("allows a single explicit rack with DefaultRackID when it has no per-rack overrides", func() {
					nsName = uniqueNamespacedName("crd-rack-id-default-explicit")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: asdbv1.DefaultRackID}},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					fetched, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())
					Expect(fetched.Spec.RackConfig.Racks).To(HaveLen(1))
					Expect(fetched.Spec.RackConfig.Racks[0].ID).To(Equal(asdbv1.DefaultRackID))
				})

				// Out-of-range above MaxRackID is rejected by CRD OpenAPI ("rejects rack id above 1000000").
				It("allows rack id at MaxRackID (in-range upper bound)", func() {
					nsName = uniqueNamespacedName("crd-rack-id-max-ok")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks:      []asdbv1.Rack{{ID: asdbv1.MaxRackID}},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					fetched, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())
					Expect(fetched.Spec.RackConfig.Racks).To(HaveLen(1))
					Expect(fetched.Spec.RackConfig.Racks[0].ID).To(Equal(asdbv1.MaxRackID))
				})
			})

			Context("negative", func() {
				It("rejects DefaultRackID when multiple racks are configured (reserved, mutating webhook)", func() {
					nsName = uniqueNamespacedName("crd-rack-id-reserved-multi")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: 1},
							{ID: asdbv1.DefaultRackID},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"maerospikecluster.kb.io\"",
							"invalid RackConfig",
							"RackID",
							"reserved",
						).
						Validate(err)
				})

				It("rejects DefaultRackID combined with rack placement (reserved, mutating webhook)", func() {
					nsName = uniqueNamespacedName("crd-rack-id-reserved-zone")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					zone := "zone-a"
					aeroCluster.Spec.RackConfig = asdbv1.RackConfig{
						Namespaces: []string{"test"},
						Racks: []asdbv1.Rack{
							{ID: asdbv1.DefaultRackID, Zone: zone},
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(
							"\"maerospikecluster.kb.io\"",
							"invalid RackConfig",
							"RackID",
							"reserved",
						).
						Validate(err)
				})
			})
		})

		Context("spec.podSpec.dnsPolicy", func() {
			Context("positive", func() {
				It("allows dnsPolicy ClusterFirst when set explicitly", func() {
					nsName = uniqueNamespacedName("crd-dns-clusterfirst")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					cf := corev1.DNSClusterFirst
					aeroCluster.Spec.PodSpec.InputDNSPolicy = &cf

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					fetched, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())
					Expect(fetched.Spec.PodSpec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
				})

				It("allows dnsPolicy None when dnsConfig is set", func() {
					nsName = uniqueNamespacedName("crd-dns-none-config")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 2)
					noneDNS := corev1.DNSNone
					aeroCluster.Spec.PodSpec.InputDNSPolicy = &noneDNS
					aeroCluster.Spec.PodSpec.DNSConfig = &corev1.PodDNSConfig{
						Nameservers: []string{"8.8.8.8"},
					}

					Expect(envtests.K8sClient.Create(ctx, aeroCluster)).To(Succeed())

					fetched, err := testCluster.GetCluster(envtests.K8sClient, ctx, nsName)
					Expect(err).ToNot(HaveOccurred())
					Expect(fetched.Spec.PodSpec.DNSPolicy).To(Equal(corev1.DNSNone))
					Expect(fetched.Spec.PodSpec.DNSConfig).ToNot(BeNil())
					Expect(fetched.Spec.PodSpec.DNSConfig.Nameservers).To(Equal([]string{"8.8.8.8"}))
				})
			})
		})

		Context("spec.seedsFinderServices.loadBalancer", func() {
			Context("negative", func() {
				It("rejects loadBalancer port below 1024 (Minimum)", func() {
					nsName = uniqueNamespacedName("crd-lb-port-min")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.SeedsFinderServices = asdbv1.SeedsFinderServices{
						LoadBalancer: &asdbv1.LoadBalancerSpec{
							Port: 1023,
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "port").
						Validate(err)
				})

				It("rejects loadBalancer targetPort above 65535 (Maximum)", func() {
					nsName = uniqueNamespacedName("crd-lb-tgt-max")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.SeedsFinderServices = asdbv1.SeedsFinderServices{
						LoadBalancer: &asdbv1.LoadBalancerSpec{
							TargetPort: 65536,
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "targetPort").
						Validate(err)
				})

				It("rejects invalid externalTrafficPolicy (Enum)", func() {
					nsName = uniqueNamespacedName("crd-lb-etp")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					bad := corev1.ServiceExternalTrafficPolicy("NotLocalOrCluster")
					aeroCluster.Spec.SeedsFinderServices = asdbv1.SeedsFinderServices{
						LoadBalancer: &asdbv1.LoadBalancerSpec{
							ExternalTrafficPolicy: bad,
						},
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "externalTrafficPolicy").
						Validate(err)
				})
			})
		})

		Context("spec.storage volumes (PersistentVolumeSpec)", func() {
			Context("negative", func() {
				It("rejects invalid volumeMode (Enum)", func() {
					nsName = uniqueNamespacedName("crd-vol-mode")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)

					for i := range aeroCluster.Spec.Storage.Volumes {
						if aeroCluster.Spec.Storage.Volumes[i].Source.PersistentVolume != nil {
							aeroCluster.Spec.Storage.Volumes[i].Source.PersistentVolume.VolumeMode =
								corev1.PersistentVolumeMode("NotBlockOrFilesystem")

							break
						}
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "volumeMode").
						Validate(err)
				})

				It("rejects accessMode not in allowed enum (items:Enum)", func() {
					nsName = uniqueNamespacedName("crd-pv-access")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)

					for i := range aeroCluster.Spec.Storage.Volumes {
						if aeroCluster.Spec.Storage.Volumes[i].Source.PersistentVolume != nil {
							aeroCluster.Spec.Storage.Volumes[i].Source.PersistentVolume.AccessModes = []corev1.PersistentVolumeAccessMode{
								corev1.ReadWriteOncePod,
							}

							break
						}
					}

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "accessModes").
						Validate(err)
				})
			})
		})

		Context("spec.storage volume policy initMethod (Enum)", func() {
			Context("negative", func() {
				It("rejects invalid block volume InputInitMethod", func() {
					nsName = uniqueNamespacedName("crd-init-method")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					bad := asdbv1.AerospikeVolumeMethod("notARealMethod")
					aeroCluster.Spec.Storage.BlockVolumePolicy.InputInitMethod = &bad

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "initMethod").
						Validate(err)
				})
			})
		})

		Context("spec.aerospikeNetworkPolicy", func() {
			Context("negative", func() {
				It("rejects invalid access type (Enum)", func() {
					nsName = uniqueNamespacedName("crd-net-access")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.AerospikeNetworkPolicy.AccessType = asdbv1.AerospikeNetworkType("notValidAccess")

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "access").
						Validate(err)
				})

				It("rejects fabric type other than customInterface when set (Enum)", func() {
					nsName = uniqueNamespacedName("crd-net-fabric")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.AerospikeNetworkPolicy.FabricType = asdbv1.AerospikeNetworkTypePod

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "fabric").
						Validate(err)
				})

				It("rejects tlsFabric type other than customInterface when set (Enum)", func() {
					nsName = uniqueNamespacedName("crd-net-tls-fabric")
					aeroCluster := testCluster.CreateDummyAerospikeCluster(nsName, 1)
					aeroCluster.Spec.AerospikeNetworkPolicy.TLSFabricType = asdbv1.AerospikeNetworkTypePod

					err := envtests.K8sClient.Create(ctx, aeroCluster)
					Expect(err).To(HaveOccurred())
					envtests.NewStatusErrorMatcher().
						WithMessageSubstrings(testutil.CRDSchemaErrorPrefix, "tlsFabric").
						Validate(err)
				})
			})
		})
	})
})
