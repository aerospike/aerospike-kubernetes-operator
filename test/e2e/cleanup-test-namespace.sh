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

# Delete rbac accounts and auth
kubectl delete clusterrolebinding aerospike-cluster
kubectl delete clusterrole aerospike-cluster
kubectl -n test delete serviceaccount aerospike-cluster
kubectl -n test1 delete serviceaccount aerospike-cluster
kubectl -n test2 delete serviceaccount aerospike-cluster

# Delete namespaces
kubectl delete namespace test1
kubectl delete namespace test2
