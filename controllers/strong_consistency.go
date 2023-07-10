package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v6"
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

func (r *SingleClusterReconciler) addedSCNamespaces(allHostConns []*deployment.HostConn) ([]string, error) {
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

	nodesNamespaces, err := deployment.GetClusterNamespaces(r.Log, r.getClientPolicy(), allHostConns)
	if err != nil {
		return nil, err
	}

	// Check if SC namespaces are present in all node's namespaces, if not then it's a new SC namespace
	for _, namespaces := range nodesNamespaces {
		nodeNamespaces := sets.NewString(namespaces...)
		newAddedSCNamespaces.Insert(specSCNamespaces.Difference(nodeNamespaces).List()...)
	}

	return newAddedSCNamespaces.List(), nil
}

func (r *SingleClusterReconciler) getIgnorableNamespaces(allHostConns []*deployment.HostConn) (
	sets.Set[string], error) {
	removedNSes, err := r.removedNamespaces(allHostConns)
	if err != nil {
		return nil, err
	}

	addSCNamespaces, err := r.addedSCNamespaces(allHostConns)
	if err != nil {
		return nil, err
	}

	ignorableNamespaces := sets.New[string](removedNSes...)
	ignorableNamespaces.Insert(addSCNamespaces...)

	return ignorableNamespaces, nil
}
