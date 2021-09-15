#!/bin/bash

################################################
# Should be run from reposiroty root
#
# Cleans up all resources created by test runs.
#
################################################

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Delete Aeropsike clusters
echo "Removing Aerospike clusters"
kubectl -n test delete aerospikecluster --all
kubectl -n test1 delete aerospikecluster --all
kubectl -n test2 delete aerospikecluster --all
kubectl -n test3 delete aerospikecluster --all

# Force delete pods
kubectl delete pod --selector 'app=aerospike-cluster' --grace-period=0 --force --namespace test || true
kubectl delete pod --selector 'app=aerospike-cluster' --grace-period=0 --force --namespace test1 || true
kubectl delete pod --selector 'app=aerospike-cluster' --grace-period=0 --force --namespace test2 || true

# Delete PVCs
echo "Removing PVCs"
kubectl -n test delete pvc --selector 'app=aerospike-cluster' || true

# Delete the secrets
echo "Removing secrets"
kubectl -n test delete secret --selector 'app=aerospike-cluster' || true

# Delete rbac accounts and auth
echo "Removing RBAC"
kubectl delete clusterrolebinding aerospike-cluster-rolebinding || true
kubectl delete clusterrole aerospike-cluster-role || true

kubectl -n test delete serviceaccount aerospike-operator-controller-manager || true
kubectl -n test1 delete serviceaccount aerospike-operator-controller-manager || true
kubectl -n test2 delete serviceaccount aerospike-operator-controller-manager || true

# Uninstall the operator
echo "Removing test operator deployment"
operator-sdk cleanup aerospike-kubernetes-operator --namespace=test

# Ensure all unlisted resources are also deleted
kubectl -n test1 delete all --all
kubectl -n test2 delete all --all
kubectl -n test delete all --all

# Delete namespaces
echo "Removing test namespaces"
kubectl delete namespace test1 || true
kubectl delete namespace test2 || true
kubectl delete namespace test || true
