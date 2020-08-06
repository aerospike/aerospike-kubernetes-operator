package e2e

import (
	"context"
	goctx "context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	as "github.com/aerospike/aerospike-client-go"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/info"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// This file needs to be changed based on setup. update zone, region, nodeName according to setup

// racks:
// - ID: 1
//   zone: us-central1-b
//   # nodeName: kubernetes-minion-group-qp3m
// - ID: 2
//   zone: us-central1-a
//   # nodeName: kubernetes-minion-group-tft3

func RackAerospikeConfigUpdateTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	// WARNING: Tests assume that only "service" is updated in aerospikeConfig, Validation is hardcoded

	t.Run("Positive", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
		// This needs to be changed based on setup. update zone, region, nodeName according to setup
		var pfd int64 = 10000
		racks := []aerospikev1alpha1.Rack{
			{
				ID:     1,
				Region: "us-central1",
				AerospikeConfig: map[string]interface{}{
					"service": map[string]interface{}{
						"proto-fd-max": pfd,
					},
				},
			},
			{
				ID:     2,
				Region: "us-central1",
			},
		}
		rackConf := aerospikev1alpha1.RackConfig{
			RackPolicy: []aerospikev1alpha1.RackPolicy{},
			Racks:      []aerospikev1alpha1.Rack{},
		}
		if err := lib.DeepCopy(&rackConf.Racks, &racks); err != nil {
			t.Fatal(err)
		}
		aeroCluster.Spec.RackConfig = rackConf

		clusterNamespacedName := getClusterNamespacedName(aeroCluster)

		t.Run("Deploy", func(t *testing.T) {
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}

			validateRackEnabledCluster(t, f, clusterNamespacedName)

			if err := validateAerospikeConfigServiceUpdate(t, f, clusterNamespacedName, racks[0]); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("AddAerospikeConfigInRack", func(t *testing.T) {
			aeroConfig := map[string]interface{}{
				"service": map[string]interface{}{
					"proto-fd-max": 12000,
				},
			}
			if err := updateAerospikeConfigInRack(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, aeroConfig); err != nil {
				t.Fatal(err)
			}
			if err := validateAerospikeConfigServiceUpdate(t, f, clusterNamespacedName, racks[0]); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("UpdateAerospikeConfigInRack", func(t *testing.T) {
			aeroConfig := map[string]interface{}{
				"service": map[string]interface{}{
					"proto-fd-max": 14000,
				},
			}
			if err := updateAerospikeConfigInRack(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, aeroConfig); err != nil {
				t.Fatal(err)
			}
			if err := validateAerospikeConfigServiceUpdate(t, f, clusterNamespacedName, racks[0]); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("RemoveAerospikeConfigInRack", func(t *testing.T) {
			aeroConfig := map[string]interface{}{}
			if err := updateAerospikeConfigInRack(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, aeroConfig); err != nil {
				t.Fatal(err)
			}
		})
		// Only updated rack's node should undergo rolling restart
		// test for namespace storage replace
		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {
		// Will be used in Update also
		t.Run("InvalidAerospikeConfig", func(t *testing.T) {

			aeroCluster := createDummyRackAwareAerospikeCluster(clusterName, namespace, 2)
			aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig = aerospikev1alpha1.Values{"namespace": "invalidConf"}
			err = deployCluster(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for invalid aerospikeConfig")

			t.Run("InvalidNamespace", func(t *testing.T) {

				t.Run("InvalidReplicationFactor", func(t *testing.T) {
					aeroCluster := createDummyRackAwareAerospikeCluster(clusterName, namespace, 2)
					aeroConfig := aerospikev1alpha1.Values{
						"namespace": []interface{}{
							map[string]interface{}{
								"name":               "test",
								"replication-factor": 3,
							},
						},
					}
					aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig = aeroConfig
					err = deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for replication-factor greater than node sz")
				})

				// Should we test for overridden fields
				t.Run("InvalidStorage", func(t *testing.T) {
					t.Run("InvalidStorageEngineDevice", func(t *testing.T) {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterName, namespace, 2)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							vd := []aerospikev1alpha1.VolumeDevice{
								aerospikev1alpha1.VolumeDevice{
									DevicePath: "/dev/xvdf1",
									SizeInGB:   1,
								},
								aerospikev1alpha1.VolumeDevice{
									DevicePath: "/dev/xvdf2",
									SizeInGB:   1,
								},
								aerospikev1alpha1.VolumeDevice{
									DevicePath: "/dev/xvdf3",
									SizeInGB:   1,
								},
							}
							aeroCluster.Spec.BlockStorage[0].VolumeDevices = append(aeroCluster.Spec.BlockStorage[0].VolumeDevices, vd...)

							aeroConfig := aerospikev1alpha1.Values{
								"namespace": []interface{}{
									map[string]interface{}{
										"name": "test",
										"storage-engine": map[string]interface{}{
											"device": []interface{}{"/dev/xvdf1 /dev/xvdf2 /dev/xvdf3"},
										},
									},
								},
							}
							aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig = aeroConfig
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for invalid storage-engine.device, cannot have 3 devices in single device string")
						}
					})

					t.Run("ExtraStorageEngineDevice", func(t *testing.T) {
						aeroCluster := createDummyRackAwareAerospikeCluster(clusterName, namespace, 2)
						if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
							aeroConfig := aerospikev1alpha1.Values{
								"namespace": []interface{}{
									map[string]interface{}{
										"name": "test",
										"storage-engine": map[string]interface{}{
											"device": []interface{}{"andRandomDevice"},
										},
									},
								},
							}
							aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig = aeroConfig
							err = deployCluster(t, f, ctx, aeroCluster)
							validateError(t, err, "should fail for invalid storage-engine.device, cannot a device which doesn't exist in BlockStorage")
						}
					})
				})

				t.Run("InvalidxdrConfig", func(t *testing.T) {
					aeroCluster := createDummyRackAwareAerospikeCluster(clusterName, namespace, 2)
					if _, ok := aeroCluster.Spec.AerospikeConfig["namespace"].([]interface{})[0].(map[string]interface{})["storage-engine"].(map[string]interface{})["device"]; ok {
						aeroCluster.Spec.FileStorage = nil
						aeroConfig := aerospikev1alpha1.Values{
							"xdr": map[string]interface{}{
								"enable-xdr":         false,
								"xdr-digestlog-path": "/opt/aerospike/xdr/digestlog 100G",
							},
						}
						aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig = aeroConfig
						err = deployCluster(t, f, ctx, aeroCluster)
						validateError(t, err, "should fail for invalid xdr config. mountPath for digestlog not present in fileStorage")
					}
				})
				// Replication-factor can not be updated
				// storage-engine can not be updated
			})
		})
	})
}

func updateAerospikeConfigInRack(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterName, namespace string, aeroConfig map[string]interface{}) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), types.NamespacedName{Name: clusterName, Namespace: namespace}, aeroCluster)
	if err != nil {
		return err
	}

	aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig = aeroConfig
	err = f.Client.Update(goctx.TODO(), aeroCluster)
	if err != nil {
		return err
	}

	// Wait for aerocluster to reach 2 replicas
	return waitForAerospikeCluster(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(aeroCluster.Spec.Size))
}

func validateAerospikeConfigServiceUpdate(t *testing.T, f *framework.Framework, namespacedName types.NamespacedName, rack aerospikev1alpha1.Rack) error {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), namespacedName, aeroCluster)
	if err != nil {
		return err
	}
	kclient := &framework.Global.Client.Client

	for _, node := range aeroCluster.Status.Nodes {

		if isNodePartOfRack(node.NodeID, strconv.Itoa(rack.ID)) {
			// TODO:
			// We may need to check for all keys in aerospikeConfig in rack
			// but we know that we are changing for service only for now
			host := &as.Host{Name: node.IP, Port: node.Port, TLSName: node.TLSName}
			asinfo := info.NewAsInfo(host, getClientPolicy(aeroCluster, kclient))
			confs, err := asinfo.GetAsConfig("service")
			svcConfs := confs["service"].(lib.Stats)
			if err != nil {
				return err
			}
			for k, v := range rack.AerospikeConfig["service"].(map[string]interface{}) {
				t.Logf("Matching rack key %s, value %v", k, v)
				cv, ok := svcConfs[k]
				if !ok {
					return fmt.Errorf("Config %s missing in aerospikeConfig %v", k, svcConfs)
				}
				if !reflect.DeepEqual(cv, getParsedValue(v)) {
					return fmt.Errorf("Config %s mismatch with config. got %v:%T, want %v:%T, aerospikeConfig %v", k, cv, cv, v, v, svcConfs)
				}

			}
		}
	}
	return nil
}

func getParsedValue(val interface{}) interface{} {

	valStr, ok := val.(string)
	if !ok {
		return val
	}

	if value, err := strconv.ParseInt(valStr, 10, 64); err == nil {
		return value
	} else if value, err := strconv.ParseFloat(valStr, 64); err == nil {
		return value
	} else if value, err := strconv.ParseBool(valStr); err == nil {
		return value
	} else {
		return valStr
	}
}

func isNodePartOfRack(nodeID string, rackID string) bool {
	// NODE_ID="$RACK_ID$NODE_ID",  NODE_ID -> aINT
	lnodeID := strings.ToLower(nodeID)
	toks := strings.Split(lnodeID, "a")
	// len(toks) can not be less than 2 if rack is there
	if rackID == toks[0] {
		return true
	}
	return false
}

// Test cluster cr updation
func RackEnabledClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	t.Run("Positive", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
		// This needs to be changed based on setup. update zone, region, nodeName according to setup
		racks := []aerospikev1alpha1.Rack{
			{ID: 1, Zone: "us-central1-b", NodeName: "kubernetes-minion-group-qp3m", Region: "us-central1"},
			{ID: 2, Zone: "us-central1-a", NodeName: "kubernetes-minion-group-tft3", Region: "us-central1"}}
		rackConf := aerospikev1alpha1.RackConfig{
			RackPolicy: []aerospikev1alpha1.RackPolicy{},
			Racks:      racks,
		}
		aeroCluster.Spec.RackConfig = rackConf

		clusterNamespacedName := getClusterNamespacedName(aeroCluster)

		t.Run("Deploy", func(t *testing.T) {
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, clusterNamespacedName)
		})
		t.Run("ScaleUp", func(t *testing.T) {
			if err := scaleUpClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, 2); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, clusterNamespacedName)
		})
		t.Run("ScaleDown", func(t *testing.T) {
			if err := scaleDownClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, 2); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, clusterNamespacedName)
		})
		t.Run("RollingRestart", func(t *testing.T) {
			if err := rollingRestartClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, clusterNamespacedName)
		})
		t.Run("Upgrade/Downgrade", func(t *testing.T) {
			// dont change build, it upgrade, check old version
			if err := upgradeClusterTest(t, f, ctx, aeroCluster.Name, aeroCluster.Namespace, buildToUpgrade); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, clusterNamespacedName)
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {
		t.Run("Deploy", func(t *testing.T) {
			t.Run("InvalidSize", func(t *testing.T) {
				aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
				rackConf := aerospikev1alpha1.RackConfig{
					RackPolicy: []aerospikev1alpha1.RackPolicy{},
					Racks:      []aerospikev1alpha1.Rack{{ID: 1}, {ID: 2}, {ID: 3}},
				}
				aeroCluster.Spec.RackConfig = rackConf
				err := deployCluster(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for InvalidSize. Cluster sz less than number of racks")
			})
			t.Run("InvalidRackID", func(t *testing.T) {
				t.Run("DuplicateRackID", func(t *testing.T) {
					aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
					rackConf := aerospikev1alpha1.RackConfig{
						RackPolicy: []aerospikev1alpha1.RackPolicy{},
						Racks:      []aerospikev1alpha1.Rack{{ID: 2}, {ID: 2}},
					}
					aeroCluster.Spec.RackConfig = rackConf
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for DuplicateRackID")
				})
				t.Run("OutOfRangeRackID", func(t *testing.T) {
					aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
					rackConf := aerospikev1alpha1.RackConfig{
						RackPolicy: []aerospikev1alpha1.RackPolicy{},
						Racks:      []aerospikev1alpha1.Rack{{ID: 1}, {ID: 20000000000}},
					}
					aeroCluster.Spec.RackConfig = rackConf
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for OutOfRangeRackID")
				})
			})
		})
		t.Run("Update", func(t *testing.T) {
			aeroCluster := createDummyAerospikeCluster(clusterName, namespace, 2)
			rackConf := aerospikev1alpha1.RackConfig{
				RackPolicy: []aerospikev1alpha1.RackPolicy{},
				Racks:      []aerospikev1alpha1.Rack{{ID: 1}, {ID: 2}},
			}
			aeroCluster.Spec.RackConfig = rackConf
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			clusterNamespacedName := getClusterNamespacedName(aeroCluster)

			t.Run("UpdateExistingRack", func(t *testing.T) {
				if err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster); err != nil {
					t.Fatal(err)
				}

				aeroCluster.Spec.RackConfig.Racks[0].Region = "randomValue"
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for updating existing rack")
			})
			t.Run("InvalidSize", func(t *testing.T) {
				if err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster); err != nil {
					t.Fatal(err)
				}

				aeroCluster.Spec.Size = 1
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for InvalidSize. Cluster sz less than number of racks")
			})
			t.Run("DuplicateRackID", func(t *testing.T) {
				if err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster); err != nil {
					t.Fatal(err)
				}

				aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, rackConf.Racks...)
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for DuplicateRackID")
			})
			t.Run("OutOfRangeRackID", func(t *testing.T) {
				if err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster); err != nil {
					t.Fatal(err)
				}

				aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, aerospikev1alpha1.Rack{ID: 20000000000})
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for OutOfRangeRackID")
			})
		})
	})
}

func validateRackEnabledCluster(t *testing.T, f *framework.Framework, clusterNamespacedName types.NamespacedName) {
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := f.Client.Get(goctx.TODO(), clusterNamespacedName, aeroCluster)
	if err != nil {
		t.Fatal(err)
	}
	// Validate cluster
	rackStateList := getNewRackStateList(aeroCluster)
	for _, rackState := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)
		err := f.Client.Get(context.TODO(), stsName, found)
		if errors.IsNotFound(err) {
			// statefulset should exist
			t.Fatal(err)
		}

		// Match size
		if int(*found.Spec.Replicas) != rackState.Size {
			t.Fatalf("statefulset replica size %d, want %d", int(*found.Spec.Replicas), rackState.Size)
		}
		t.Logf("matched statefulset replica size with required rack size %d", rackState.Size)

		// If Label key are changed for zone, region.. then those should be changed here also

		// Match NodeAffinity, if something else is used in place of affinity then it will fail
		validateSTSForRack(t, f, found, rackState)

		// Match Pod's Node
		validateSTSPodsForRack(t, f, found, rackState)
	}
}

func validateSTSForRack(t *testing.T, f *framework.Framework, found *appsv1.StatefulSet, rackState RackState) {

	zoneKey := "failure-domain.beta.kubernetes.io/zone"
	regionKey := "failure-domain.beta.kubernetes.io/region"
	rackLabelKey := "RackLabel"
	hostKey := "kubernetes.io/hostname"

	rackSelectorMap := map[string]string{}
	if rackState.Rack.Zone != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      zoneKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.Zone},
		}
		rackSelectorMap[zoneKey] = val.String()
	}
	if rackState.Rack.Region != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      regionKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.Region},
		}
		rackSelectorMap[regionKey] = val.String()
	}
	if rackState.Rack.RackLabel != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      rackLabelKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.RackLabel},
		}
		rackSelectorMap[rackLabelKey] = val.String()
	}
	if rackState.Rack.NodeName != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      hostKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.NodeName},
		}
		rackSelectorMap[hostKey] = val.String()
	}

	if len(rackSelectorMap) == 0 {
		return
	}

	terms := found.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

	var matched bool
	for _, term := range terms {
		expMap := map[string]string{}
		for _, exp := range term.MatchExpressions {
			expMap[exp.Key] = exp.String()
		}
		matched = reflect.DeepEqual(rackSelectorMap, expMap)
		if matched {
			break
		}
	}
	if !matched {
		t.Fatalf("statefulset doesn't have required match strings. terms %v", terms)
	}
	t.Logf("matched statefulset selector terms %v", terms)
}

func validateSTSPodsForRack(t *testing.T, f *framework.Framework, found *appsv1.StatefulSet, rackState RackState) {

	zoneKey := "failure-domain.beta.kubernetes.io/zone"
	regionKey := "failure-domain.beta.kubernetes.io/region"
	rackLabelKey := "RackLabel"
	hostKey := "kubernetes.io/hostname"

	rackSelectorMap := map[string]string{}
	if rackState.Rack.Zone != "" {
		rackSelectorMap[zoneKey] = rackState.Rack.Zone
	}
	if rackState.Rack.Region != "" {
		rackSelectorMap[regionKey] = rackState.Rack.Region
	}
	if rackState.Rack.RackLabel != "" {
		rackSelectorMap[rackLabelKey] = rackState.Rack.RackLabel
	}
	if rackState.Rack.NodeName != "" {
		rackSelectorMap[hostKey] = rackState.Rack.NodeName
	}

	rackPodList, err := getRackPodList(f, found)
	if err != nil {
		t.Fatal(err)
	}
	for _, pod := range rackPodList.Items {
		node := &corev1.Node{}
		err := f.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, node)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("Pod's node %s and labels %v", pod.Spec.NodeName, node.Labels)

		for k, v1 := range rackSelectorMap {
			if v2, ok := node.Labels[k]; !ok {
				// error
				t.Fatalf("Rack key %s, not present in node labels %v", k, node.Labels)
			} else if v1 != v2 {
				// error
				t.Fatalf("Rack key:val %s:%s doesn't match in node labels %v", k, v1, node.Labels)
			}
		}
	}
}
