package e2e

import (
	"context"
	"reflect"
	"testing"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
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

// Test cluster cr updation
func RackEnabledClusterTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	t.Run("Positive", func(t *testing.T) {
		// Will be used in Update also
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
		// This needs to be changed based on setup. update zone, region, nodeName according to setup
		racks := []aerospikev1alpha1.Rack{
			{ID: 1, Zone: "us-central1-b", NodeName: "kubernetes-minion-group-qp3m", Region: "us-central1"},
			{ID: 2, Zone: "us-central1-a", NodeName: "kubernetes-minion-group-tft3", Region: "us-central1"}}
		rackConf := aerospikev1alpha1.RackConfig{
			Racks: racks,
		}
		aeroCluster.Spec.RackConfig = rackConf

		t.Run("Deploy", func(t *testing.T) {
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
		t.Run("ScaleUp", func(t *testing.T) {
			if err := scaleUpClusterTest(t, f, ctx, clusterNamespacedName, 2); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
		t.Run("ScaleDown", func(t *testing.T) {
			if err := scaleDownClusterTest(t, f, ctx, clusterNamespacedName, 2); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
		t.Run("RollingRestart", func(t *testing.T) {
			if err := rollingRestartClusterTest(t, f, ctx, clusterNamespacedName); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})
		t.Run("Upgrade/Downgrade", func(t *testing.T) {
			// dont change image, it upgrade, check old version
			if err := upgradeClusterTest(t, f, ctx, clusterNamespacedName, imageToUpgrade); err != nil {
				t.Fatal(err)
			}
			validateRackEnabledCluster(t, f, ctx, clusterNamespacedName)
		})

		deleteCluster(t, f, ctx, aeroCluster)
	})

	t.Run("Negative", func(t *testing.T) {
		t.Run("Deploy", func(t *testing.T) {
			t.Run("InvalidSize", func(t *testing.T) {
				aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
				rackConf := aerospikev1alpha1.RackConfig{
					Racks: []aerospikev1alpha1.Rack{{ID: 1}, {ID: 2}, {ID: 3}},
				}
				aeroCluster.Spec.RackConfig = rackConf
				err := deployCluster(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for InvalidSize. Cluster sz less than number of racks")
			})
			t.Run("InvalidRackID", func(t *testing.T) {
				t.Run("DuplicateRackID", func(t *testing.T) {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					rackConf := aerospikev1alpha1.RackConfig{
						Racks: []aerospikev1alpha1.Rack{{ID: 2}, {ID: 2}},
					}
					aeroCluster.Spec.RackConfig = rackConf
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for DuplicateRackID")
				})
				t.Run("OutOfRangeRackID", func(t *testing.T) {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					rackConf := aerospikev1alpha1.RackConfig{
						Racks: []aerospikev1alpha1.Rack{{ID: 1}, {ID: utils.MaxRackID + 1}},
					}
					aeroCluster.Spec.RackConfig = rackConf
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for OutOfRangeRackID")
				})
				t.Run("UseDefaultRackID", func(t *testing.T) {
					aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
					rackConf := aerospikev1alpha1.RackConfig{
						Racks: []aerospikev1alpha1.Rack{{ID: 1}, {ID: utils.DefaultRackID}},
					}
					aeroCluster.Spec.RackConfig = rackConf
					err := deployCluster(t, f, ctx, aeroCluster)
					validateError(t, err, "should fail for using defaultRackID")
				})
			})
		})
		t.Run("Update", func(t *testing.T) {
			aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
			rackConf := aerospikev1alpha1.RackConfig{
				Racks: []aerospikev1alpha1.Rack{{ID: 1}, {ID: 2}},
			}
			aeroCluster.Spec.RackConfig = rackConf
			if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
			// clusterNamespacedName := getClusterNamespacedName(aeroCluster)

			t.Run("UpdateExistingRack", func(t *testing.T) {
				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.RackConfig.Racks[0].Region = "randomValue"
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for updating existing rack")
			})
			t.Run("InvalidSize", func(t *testing.T) {
				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.Size = 1
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for InvalidSize. Cluster sz less than number of racks")
			})
			t.Run("DuplicateRackID", func(t *testing.T) {
				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, rackConf.Racks...)
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for DuplicateRackID")
			})
			t.Run("OutOfRangeRackID", func(t *testing.T) {
				aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
				aeroCluster.Spec.RackConfig.Racks = append(aeroCluster.Spec.RackConfig.Racks, aerospikev1alpha1.Rack{ID: 20000000000})
				err = updateAndWait(t, f, ctx, aeroCluster)
				validateError(t, err, "should fail for OutOfRangeRackID")
			})
			deleteCluster(t, f, ctx, aeroCluster)
		})
	})
}

func validateRackEnabledCluster(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, clusterNamespacedName types.NamespacedName) {
	aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)

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
