package controllers

import (
	corev1 "k8s.io/api/core/v1"

	as "github.com/aerospike/aerospike-client-go/v6"
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

	removedNSes, err := r.removedNamespaces(allHostConns)
	if err != nil {
		return err
	}

	return deployment.GetAndSetRoster(r.Log, allHostConns, policy, rosterNodeBlockList, removedNSes)
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
