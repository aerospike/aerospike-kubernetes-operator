package controllers

import (
	gosets "github.com/deckarep/golang-set/v2"
	"k8s.io/apimachinery/pkg/util/sets"

	as "github.com/aerospike/aerospike-client-go/v6"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

func (r *SingleClusterReconciler) getAndSetRoster(
	policy *as.ClientPolicy, rosterNodeBlockList []string,
	ignorablePodNames sets.Set[string],
) error {
	allHostConns, err := r.newAllHostConnWithOption(ignorablePodNames)
	if err != nil {
		return err
	}

	ignorableNamespaces, err := r.getIgnorableNamespaces(allHostConns)
	if err != nil {
		return err
	}

	return deployment.GetAndSetRoster(r.Log, allHostConns, policy, rosterNodeBlockList, ignorableNamespaces)
}

func (r *SingleClusterReconciler) validateSCClusterState(policy *as.ClientPolicy, ignorablePodNames sets.Set[string],
) error {
	allHostConns, err := r.newAllHostConnWithOption(ignorablePodNames)
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
		specSCNamespaces     = gosets.NewSet[string]()
		newAddedSCNamespaces = gosets.NewSet[string]()
	)

	// Look inside only 1st rack. SC namespaces should be same across all the racks
	rack := r.aeroCluster.Spec.RackConfig.Racks[0]
	nsList := rack.AerospikeConfig.Value["namespaces"].([]interface{})

	for _, nsConfInterface := range nsList {
		if asdbv1.IsNSSCEnabled(nsConfInterface.(map[string]interface{})) {
			specSCNamespaces.Add(nsConfInterface.(map[string]interface{})["name"].(string))
		}
	}

	// Check if SC namespaces are present in all node's namespaces, if not then it's a new SC namespace
	for _, namespaces := range nodesNamespaces {
		nodeNamespaces := gosets.NewSet[string](namespaces...)
		newAddedSCNamespaces.Append(specSCNamespaces.Difference(nodeNamespaces).ToSlice()...)
	}

	return newAddedSCNamespaces.ToSlice()
}

func (r *SingleClusterReconciler) getIgnorableNamespaces(allHostConns []*deployment.HostConn) (
	gosets.Set[string], error) {
	nodesNamespaces, err := deployment.GetClusterNamespaces(r.Log, r.getClientPolicy(), allHostConns)
	if err != nil {
		return nil, err
	}

	removedNamespaces := r.removedNamespaces(nodesNamespaces)
	addSCNamespaces := r.addedSCNamespaces(nodesNamespaces)

	ignorableNamespaces := gosets.NewSet[string](removedNamespaces...)
	ignorableNamespaces.Append(addSCNamespaces...)

	return ignorableNamespaces, nil
}
