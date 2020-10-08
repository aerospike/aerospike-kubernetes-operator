#!/usr/bin/env bash

# cluster1

kubectl create namespace aerospike1
sleep 2

kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike1
sleep 2

kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike1
sleep 2

# cluster2

kubectl create namespace aerospike2
sleep 2

kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike2
sleep 2

kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike2
sleep 2

# cluster3

kubectl create namespace aerospike3
sleep 2

kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike3
sleep 2

kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike3
sleep 2

# rbac for all cluster
kubectl apply -f deploy/rbac_cluster.yaml
sleep 2

# kubectl apply -f deploy/samples/hdd_dim_storage_cluster_cr.yaml
