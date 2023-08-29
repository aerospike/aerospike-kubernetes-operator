package controllers

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v6"
)

func (r *SingleClusterReconciler) getAndSetRoster(
	policy *as.ClientPolicy, ignorablePods []corev1.Pod,
) error {
	allHostConns, err := r.newAllHostConnWithOption(ignorablePods)
	if err != nil {
		return err
	}

	removedNSes, err := r.removedNamespaces(allHostConns)
	if err != nil {
		return err
	}

	return deployment.GetAndSetRoster(r.Log, allHostConns, policy, r.aeroCluster.Spec.RosterNodeBlockList, removedNSes)
}

func (r *SingleClusterReconciler) validateSCClusterState(policy *as.ClientPolicy, ignorablePods []corev1.Pod) error {
	allHostConns, err := r.newAllHostConnWithOption(ignorablePods)
	if err != nil {
		return err
	}

	removedNSes, err := r.removedNamespaces(allHostConns)
	if err != nil {
		return err
	}

	return deployment.ValidateSCClusterState(r.Log, allHostConns, policy, removedNSes)
}
