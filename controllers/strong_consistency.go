package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	as "github.com/aerospike/aerospike-client-go/v6"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

func (r *SingleClusterReconciler) getAndSetRoster(
	policy *as.ClientPolicy, rosterNodeBlockList []string,
	ignorablePods []corev1.Pod,
) error {
	allHostConns, err := r.newAllHostConnWithOption(ignorablePods)
	if err != nil {
		return err
	}

	ignorableNamespaces, err := r.getIgnorableNamespaces(allHostConns)
	if err != nil {
		return err
	}

	return deployment.GetAndSetRoster(r.Log, allHostConns, policy, rosterNodeBlockList, ignorableNamespaces)
}

func (r *SingleClusterReconciler) validateSCClusterState(policy *as.ClientPolicy, ignorablePods []corev1.Pod) error {
	allHostConns, err := r.newAllHostConnWithOption(ignorablePods)
	if err != nil {
		return err
	}

	ignorableNamespaces, err := r.getIgnorableNamespaces(allHostConns)
	if err != nil {
		return err
	}

	return deployment.ValidateSCClusterState(r.Log, allHostConns, policy, ignorableNamespaces)
}

func (r *SingleClusterReconciler) addedSCNamespaces(nodesNamespaces map[string][]string) []string {
	var (
		specSCNamespaces     = sets.NewString()
		newAddedSCNamespaces = sets.NewString()
	)

	// Look inside only 1st rack. SC namespaces should be same across all the racks
	rack := r.aeroCluster.Spec.RackConfig.Racks[0]
	nsList := rack.AerospikeConfig.Value["namespaces"].([]interface{})

	for _, nsConfInterface := range nsList {
		if asdbv1.IsNSSCEnabled(nsConfInterface.(map[string]interface{})) {
			specSCNamespaces.Insert(nsConfInterface.(map[string]interface{})["name"].(string))
		}
	}

	// Check if SC namespaces are present in all node's namespaces, if not then it's a new SC namespace
	for _, namespaces := range nodesNamespaces {
		nodeNamespaces := sets.NewString(namespaces...)
		newAddedSCNamespaces.Insert(specSCNamespaces.Difference(nodeNamespaces).List()...)
	}

	return newAddedSCNamespaces.List()
}

func (r *SingleClusterReconciler) getIgnorableNamespaces(allHostConns []*deployment.HostConn) (
	sets.Set[string], error) {
	nodesNamespaces, err := deployment.GetClusterNamespaces(r.Log, r.getClientPolicy(), allHostConns)
	if err != nil {
		return nil, err
	}

	removedNamespaces := r.removedNamespaces(nodesNamespaces)
	addSCNamespaces := r.addedSCNamespaces(nodesNamespaces)

	ignorableNamespaces := sets.New[string](removedNamespaces...)
	ignorableNamespaces.Insert(addSCNamespaces...)

	return ignorableNamespaces, nil
}
