#!/bin/bash
#
# Cleans up all resources created by test runs.
#

# Delete Aeropsike clusters
kubectl -n test delete aerospikecluster --all
kubectl -n test1 delete aerospikecluster --all
kubectl -n test2 delete aerospikecluster --all
kubectl -n test3 delete aerospikecluster --all

kubectl delete -f test/e2e/setup_operator_test.yaml

kubectl delete clusterrolebinding aerospike-cluster
kubectl delete clusterrole aerospike-cluster

# Delete namespaces
kubectl delete namespace test test1 test2 test3

# # Delete Stateful Sets
# kubectl -n test delete statefulset --selector 'app=aerospike-cluster'

# # Delete PVCs
# kubectl -n test delete pvc --selector 'app=aerospike-cluster'

# # Delete the secrets
# kubectl -n test delete secret --selector 'app=aerospike-cluster'

# # Delete rbac accounts and auth
# kubectl delete clusterrolebinding aerospike-cluster
# kubectl delete clusterrole aerospike-cluster
# kubectl -n test delete serviceaccount aerospike-cluster
# kubectl -n test1 delete serviceaccount aerospike-cluster
# kubectl -n test2 delete serviceaccount aerospike-cluster

# # Delete namespaces
# kubectl delete namespace test test1 test2 test3
