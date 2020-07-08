package e2e

import (
	"context"
	"strconv"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RackState struct {
	Rack aerospikev1alpha1.Rack
	Size int
}

func getNewRackStateList(aeroCluster *aerospikev1alpha1.AerospikeCluster) []RackState {
	topology := splitRacks(int(aeroCluster.Spec.Size), len(aeroCluster.Spec.RackConfig.Racks))
	var rackStateList []RackState
	for idx, rack := range aeroCluster.Spec.RackConfig.Racks {
		rackStateList = append(rackStateList, RackState{
			Rack: rack,
			Size: topology[idx],
		})
	}
	return rackStateList
}

// TODO: Update this
func splitRacks(nodeCount, rackCount int) []int {
	nodesPerRack, extraNodes := nodeCount/rackCount, nodeCount%rackCount

	var topology []int

	for rackIdx := 0; rackIdx < rackCount; rackIdx++ {
		nodesForThisRack := nodesPerRack
		if rackIdx < extraNodes {
			nodesForThisRack++
		}
		topology = append(topology, nodesForThisRack)
	}

	return topology
}

func getNamespacedNameForStatefulSet(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackID int) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func getClusterNamespacedName(aeroCluster *aerospikev1alpha1.AerospikeCluster) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name,
		Namespace: aeroCluster.Namespace,
	}
}

func getRackPodList(f *framework.Framework, found *appsv1.StatefulSet) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(found.Spec.Template.Labels)
	listOps := &client.ListOptions{Namespace: found.Namespace, LabelSelector: labelSelector}

	if err := f.Client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}
