#!/bin/bash
#
# Cleans up all resources created by test runs.
#

# Delete Aeropsike clusters
kubectl -n test delete aerospikecluster --all

# Delete Stateful Sets
kubectl -n test delete statefulset --selector 'app=aerospike-cluster'

# Delete PVCs
kubectl -n test delete pvc --selector 'app=aerospike-cluster'

# Delete the secrets
kubectl -n test delete secret --selector 'app=aerospike-cluster'
