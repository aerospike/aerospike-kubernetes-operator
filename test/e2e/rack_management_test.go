package e2e

import (
	goctx "context"
	"reflect"
	"strconv"
	"testing"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/info"
	as "github.com/ashishshinde/aerospike-client-go"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/types"
)

// Test cluster cr updation
func RackManagementTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	t.Run("Positive", func(t *testing.T) {
		// Deploy cluster without rack
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		// Add new rack
		t.Run("AddRack", func(t *testing.T) {
			t.Run("Add1stRackOfCluster", func(t *testing.T) {
				addRack(t, f, ctx, clusterNamespacedName, aerospikev1alpha1.Rack{ID: 1})
				validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
			})

			t.Run("AddRackInExistingRacks", func(t *testing.T) {
				addRack(t, f, ctx, clusterNamespacedName, aerospikev1alpha1.Rack{ID: 2})
				validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
			})
		})

		t.Run("UpdateRackEnabledNamespace", func(t *testing.T) {
			nsListInterface := aeroCluster.Spec.AerospikeConfig["namespaces"]
			nsList := nsListInterface.([]interface{})
			nsName := nsList[0].(map[string]interface{})["name"].(string)

			t.Run("AddRackEnabledNamespace", func(t *testing.T) {

				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.RackConfig.Namespaces = []string{nsName}

				if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}
				validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
				// Check if namespace has became rackEnabled
				enabled := isNamespaceRackEnabled(t, f, ctx, clusterNamespacedName, nsName)
				if !enabled {
					t.Fatalf("Couldn't find ns in rackEnabled namespace list. It should have been there")
				}
			})

			t.Run("RemoveRackEnabledNamespace", func(t *testing.T) {

				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.RackConfig.Namespaces = []string{}

				if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}
				validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
				// Check if namespace has became rackDisabled
				enabled := isNamespaceRackEnabled(t, f, ctx, clusterNamespacedName, nsName)
				if enabled {
					t.Fatalf("Found ns in rackEnabled namespace list. It should have been removed")
				}
			})
		})

		// Remove rack
		t.Run("RemoveRack", func(t *testing.T) {
			t.Run("RemoveSingleRack", func(t *testing.T) {
				removeLastRack(t, f, ctx, clusterNamespacedName)
				validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
			})

			t.Run("RemoveAllRacks", func(t *testing.T) {
				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

				rackConf := aerospikev1alpha1.RackConfig{}
				aeroCluster.Spec.RackConfig = rackConf

				// This will also indirectl check if older rack is removed or not.
				// If older node is not deleted then cluster sz will not be as expected
				if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
					t.Fatal(err)
				}
				validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
			})
		})

		// Update not allowed

		deleteCluster(t, f, ctx, aeroCluster)
	})
}

func RackAerospikeConfigUpdateTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	// WARNING: Tests assume that only "service" is updated in aerospikeConfig, Validation is hardcoded
	t.Run("Positive", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
		racks := getDummyRackConf(1, 2)

		t.Run("DeployWithAerospikeConfig", func(t *testing.T) {

			racks[0].InputAerospikeConfig = &v1alpha1.Values{
				"service": map[string]interface{}{
					"proto-fd-max": 10000,
				},
			}
			racks[1].InputAerospikeConfig = &v1alpha1.Values{
				"service": map[string]interface{}{
					"proto-fd-max": 12000,
				},
			}

			// Make a copy to validate later
			racksCopy := []aerospikev1alpha1.Rack{}
			if err := Copy(&racksCopy, &racks); err != nil {
				t.Fatal(err)
			}

			aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racksCopy}
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)

			for _, rack := range racks {
				validateAerospikeConfigServiceUpdate(t, f, ctx, clusterNamespacedName, rack)
			}
		})

		t.Run("UpdateAerospikeConfigInRack", func(t *testing.T) {
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

			racks[0].InputAerospikeConfig = &v1alpha1.Values{
				"service": map[string]interface{}{
					"proto-fd-max": 12000,
				},
			}
			racks[1].InputAerospikeConfig = &v1alpha1.Values{
				"service": map[string]interface{}{
					"proto-fd-max": 14000,
				},
			}

			// Make a copy to validate later
			racksCopy := []aerospikev1alpha1.Rack{}
			if err := Copy(&racksCopy, &racks); err != nil {
				t.Fatal(err)
			}

			aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racksCopy}

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)

			for _, rack := range racks {
				validateAerospikeConfigServiceUpdate(t, f, ctx, clusterNamespacedName, rack)
			}
		})

		t.Run("RemoveAerospikeConfigInRack", func(t *testing.T) {
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
			racks[0].InputAerospikeConfig = nil
			racks[1].InputAerospikeConfig = nil

			// Make a copy to validate later
			racksCopy := []aerospikev1alpha1.Rack{}
			if err := Copy(&racksCopy, &racks); err != nil {
				t.Fatal(err)
			}

			aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racksCopy}
			// Increase size also so that below wait func wait for new cluster
			aeroCluster.Spec.Size = aeroCluster.Spec.Size + 1

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)

			// Config for both rack should have been taken from default config
			// Default proto-fd-max is 15000. So check for default value
			racks[0].InputAerospikeConfig = &v1alpha1.Values{
				"service": map[string]interface{}{
					"proto-fd-max": defaultProtofdmax,
				},
			}
			racks[1].InputAerospikeConfig = &v1alpha1.Values{
				"service": map[string]interface{}{
					"proto-fd-max": defaultProtofdmax,
				},
			}
			for _, rack := range racks {
				validateAerospikeConfigServiceUpdate(t, f, ctx, clusterNamespacedName, rack)
			}
		})
		// Only updated rack's node should undergo rolling restart
		// test for namespace storage replace
		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {

		t.Run("Deploy", func(t *testing.T) {
			t.Run("InvalidAerospikeConfig", func(t *testing.T) {

				aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
				aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aerospikev1alpha1.Values{"namespaces": "invalidConf"}
				err = deployCluster(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for invalid aerospikeConfig")

				t.Run("InvalidNamespace", func(t *testing.T) {

					t.Run("InvalidReplicationFactor", func(t *testing.T) {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						aeroConfig := aerospikev1alpha1.Values{
							"namespaces": []interface{}{
								map[string]interface{}{
									"name":               "test",
									"replication-factor": 3,
								},
							},
						}
						aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aeroConfig
						err = deployCluster(t, f, ctx, aeroCluster)
						validateError(t, err, "should fail for replication-factor greater than node sz")
					})

					// Should we test for overridden fields
					t.Run("InvalidStorage", func(t *testing.T) {
						t.Run("InvalidStorageEngineDevice", func(t *testing.T) {
							aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
							if _, ok := aeroCluster.Spec.AerospikeConfig["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
								vd := []aerospikev1alpha1.AerospikePersistentVolumeSpec{
									aerospikev1alpha1.AerospikePersistentVolumeSpec{
										Path:       "/dev/xvdf1",
										SizeInGB:   1,
										VolumeMode: aerospikev1alpha1.AerospikeVolumeModeBlock,
									},
									aerospikev1alpha1.AerospikePersistentVolumeSpec{
										Path:       "/dev/xvdf2",
										SizeInGB:   1,
										VolumeMode: aerospikev1alpha1.AerospikeVolumeModeBlock,
									},
									aerospikev1alpha1.AerospikePersistentVolumeSpec{
										Path:       "/dev/xvdf3",
										SizeInGB:   1,
										VolumeMode: aerospikev1alpha1.AerospikeVolumeModeBlock,
									},
								}
								aeroCluster.Spec.Storage.Volumes = append(aeroCluster.Spec.Storage.Volumes, vd...)

								aeroConfig := aerospikev1alpha1.Values{
									"namespaces": []interface{}{
										map[string]interface{}{
											"name": "test",
											"storage-engine": map[string]interface{}{
												"type":    "device",
												"devices": []interface{}{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"},
											},
										},
									},
								}
								aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aeroConfig
								err = deployCluster(t, f, ctx, aeroCluster)
								validateError(t, err, "should fail for invalid storage-engine.device, cannot have 3 devices in single device string")
							}
						})

						t.Run("ExtraStorageEngineDevice", func(t *testing.T) {
							aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
							if _, ok := aeroCluster.Spec.AerospikeConfig["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
								aeroConfig := aerospikev1alpha1.Values{
									"namespaces": []interface{}{
										map[string]interface{}{
											"name": "test",
											"storage-engine": map[string]interface{}{
												"type":    "device",
												"devices": []interface{}{"andRandomDevice"},
											},
										},
									},
								}
								aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aeroConfig
								err = deployCluster(t, f, ctx, aeroCluster)
								validateError(t, err, "should fail for invalid storage-engine.device, cannot a device which doesn't exist in BlockStorage")
							}
						})
					})

					t.Run("InvalidxdrConfig", func(t *testing.T) {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["devices"]; ok {
							aeroCluster.Spec.Storage = aerospikev1alpha1.AerospikeStorageSpec{}
							aeroConfig := aerospikev1alpha1.Values{
								"xdr": map[string]interface{}{
									"enable-xdr":         false,
									"xdr-digestlog-path": "/opt/aerospike/xdr/digestlog 100G",
								},
							}
							aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &aeroConfig
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for invalid xdr config. mountPath for digestlog not present in fileStorage")
						}
					})
					// Replication-factor can not be updated
				})
			})
		})

		t.Run("Update", func(t *testing.T) {
			t.Run("InvalidAerospikeConfig", func(t *testing.T) {

				// storage-engine can not be updated
				t.Run("UpdateStorageEngine", func(t *testing.T) {
					clusterName := "updatestorage"
					clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

					t.Run("NonEmptyToEmpty", func(t *testing.T) {
						// Deploy cluster with rackConfig
						aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
						rackAeroConf := aerospikev1alpha1.Values{}

						Copy(&rackAeroConf, &aeroCluster.Spec.AerospikeConfig)
						rackAeroConf["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["type"] = "memory"

						racks := []aerospikev1alpha1.Rack{{ID: 1, InputAerospikeConfig: &rackAeroConf}}
						aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: racks}

						if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
							t.Fatal(err)
						}

						// Update rackConfig to nil. should fail as storage can not be updated
						aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
						aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = nil
						err = f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating storage")

						deleteCluster(t, f, ctx, aeroCluster)
					})
					t.Run("EmptyToNonEmpty", func(t *testing.T) {
						// Deploy cluster with empty rackConfig
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterNamespacedName, 2)
						if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
							t.Fatal(err)
						}

						// Update rackConfig to nonEmpty. should fail as storage can not be updated
						aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
						rackAeroConf := aerospikev1alpha1.Values{}
						Copy(&rackAeroConf, &aeroCluster.Spec.AerospikeConfig)
						rackAeroConf["namespaces"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["type"] = "memory"

						aeroCluster.Spec.RackConfig.Racks[0].InputAerospikeConfig = &rackAeroConf
						err = f.Client.Update(goctx.TODO(), aeroCluster)
						validateError(t, err, "should fail for updating storage")

						deleteCluster(t, f, ctx, aeroCluster)
					})
				})
			})
		})
	})
}

func addRack(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName, rack aerospikev1alpha1.Rack) {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	// Remove default rack
	defaultRackID := utils.DefaultRackID
	if len(aeroCluster.Spec.RackConfig.Racks) == 1 && aeroCluster.Spec.RackConfig.Racks[0].ID == defaultRackID {
		aeroCluster.Spec.RackConfig = aerospikev1alpha1.RackConfig{Racks: []aerospikev1alpha1.Rack{}}
	}

	aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, rack)
	// Size shouldn't make any difference in working. Still put different size to check if it create any issue.
	aeroCluster.Spec.Size = aeroCluster.Spec.Size + 1
	if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
		t.Fatal(err)
	}
}

func removeLastRack(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	racks := aeroCluster.Spec.RackConfig.Racks
	if len(racks) > 0 {
		racks = racks[:len(racks)-1]
	}

	aeroCluster.Spec.RackConfig.Racks = racks
	aeroCluster.Spec.Size = aeroCluster.Spec.Size - 1
	// This will also indirectl check if older rack is removed or not.
	// If older node is not deleted then cluster sz will not be as expected
	if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
		t.Fatal(err)
	}
}

func validateAerospikeConfigServiceUpdate(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName, rack aerospikev1alpha1.Rack) {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	kclient := &framework.Global.Client.Client

	var found bool
	for _, pod := range aeroCluster.Status.Pods {

		if isNodePartOfRack(pod.Aerospike.NodeID, strconv.Itoa(rack.ID)) {
			found = true
			// TODO:
			// We may need to check for all keys in aerospikeConfig in rack
			// but we know that we are changing for service only for now
			host := &as.Host{Name: pod.HostExternalIP, Port: int(pod.ServicePort), TLSName: pod.Aerospike.TLSName}
			asinfo := info.NewAsInfo(host, getClientPolicy(aeroCluster, kclient))
			confs, err := asinfo.GetAsConfig("service")
			if err != nil {
				t.Fatal(err)
			}
			svcConfs := confs["service"].(lib.Stats)

			for k, v := range (*rack.InputAerospikeConfig)["service"].(map[string]interface{}) {
				if vint, ok := v.(int); ok {
					v = int64(vint)
				}
				t.Logf("Matching rack key %s, value %v", k, v)
				cv, ok := svcConfs[k]
				if !ok {
					t.Fatalf("Config %s missing in aerospikeConfig %v", k, svcConfs)
				}
				if !reflect.DeepEqual(cv, v) {
					t.Fatalf("Config %s mismatch with config. got %v:%T, want %v:%T, aerospikeConfig %v", k, cv, cv, v, v, svcConfs)
				}

			}
		}
	}
	if !found {
		t.Fatalf("No pod found in for rack. Pods %v, Rack %v", aeroCluster.Status.Pods, rack)
	}
}

func isNamespaceRackEnabled(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName, nsName string) bool {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

	kclient := &framework.Global.Client.Client

	if len(aeroCluster.Status.Pods) == 0 {
		t.Fatal("Cluster has empty pod list in status")
	}

	var pod aerospikev1alpha1.AerospikePodStatus
	for _, p := range aeroCluster.Status.Pods {
		pod = p
	}
	host := &as.Host{Name: pod.HostExternalIP, Port: int(pod.ServicePort), TLSName: pod.Aerospike.TLSName}
	asinfo := info.NewAsInfo(host, getClientPolicy(aeroCluster, kclient))

	confs, err := asinfo.GetAsConfig("racks")
	if err != nil {
		t.Fatal(err)
	}
	for _, rackConf := range confs["racks"].([]lib.Stats) {
		// rack_0 is form non-rack namespace. So if rack_0 is present then it's not rack enabled
		_, ok := rackConf["rack_0"]

		ns := rackConf.TryString("ns", "")
		if ns == nsName && !ok {
			return true
		}
	}

	return false
}
