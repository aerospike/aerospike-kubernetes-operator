package controllers

import (
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v6"
)

func (r *SingleClusterReconciler) getAndSetRoster(policy *as.ClientPolicy, rosterBlockList []string) error {
	hostConns, err := r.newAllHostConn()
	if err != nil {
		return err
	}

	removedNSes, err := r.removedNamespaces()
	if err != nil {
		return err
	}

	return deployment.GetAndSetRoster(r.Log, hostConns, policy, rosterBlockList, removedNSes)
}

func (r *SingleClusterReconciler) validateSCClusterState(policy *as.ClientPolicy) error {
	hostConns, err := r.newAllHostConn()
	if err != nil {
		return err
	}

	removedNSes, err := r.removedNamespaces()
	if err != nil {
		return err
	}

	return deployment.ValidateSCClusterState(r.Log, hostConns, policy, removedNSes)
}
