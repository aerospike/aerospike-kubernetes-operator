#!/bin/bash

################################################
# Should be run from reposiroty root
#
# Cleans up all resources created by test runs.
#
################################################

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Delete Aeropsike clusters
kubectl -n test delete aerospikecluster --all

# Delete Stateful Sets
kubectl -n test delete statefulset --selector 'app=aerospike-cluster'

# Delete PVCs
kubectl -n test delete pvc --selector 'app=aerospike-cluster'

# Delete the secrets
kubectl -n test delete secret --selector 'app=aerospike-cluster' || true

# Delete rbac accounts and auth
kubectl delete clusterrolebinding aerospike-cluster || true
kubectl delete clusterrole aerospike-cluster || true
kubectl -n test delete serviceaccount aerospike-cluster || true
kubectl -n test1 delete serviceaccount aerospike-cluster || true
kubectl -n test2 delete serviceaccount aerospike-cluster || true

# Delete the operator deployment
kubectl -n test delete -f $DIR/setup_operator_test.yaml || true

# Delete namespaces
kubectl delete namespace test1 || true
kubectl delete namespace test2 || true
kubectl delete namespace test || true
