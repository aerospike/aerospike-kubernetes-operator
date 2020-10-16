#!/usr/bin/env bash

# cluster1
kubectl create namespace aerospike
sleep 2

kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike
sleep 2

kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike
sleep 2

# # Uncomment below sections for deploying clusters in additional namespaces 

# # cluster2
# kubectl create namespace aerospike1
# sleep 2

# kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike1
# sleep 2

# kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike1
# sleep 2

# # cluster3
# kubectl create namespace aerospike2
# sleep 2

# kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike2
# sleep 2

# kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike2
# sleep 2

