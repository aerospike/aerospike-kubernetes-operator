#! /usr/bin/env bash

kubectl delete -f deploy/samples/hdd_dim_storage_cluster_cr.yaml
sleep 10

kubectl delete -f deploy/rbac.yaml
kubectl delete -f deploy/operator.yaml
kubectl delete -f deploy/crds/aerospike.com_aerospikeclusters_crd.yaml
kubectl delete secret auth-secret
kubectl delete secret aerospike-secret
kubectl delete secret auth
