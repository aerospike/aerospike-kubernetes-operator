
#!/usr/bin/env bash

kubectl apply -f deploy/storage_class.yaml
sleep 2

# Setup operator
kubectl create namespace aerospike
sleep 2

kubectl apply -f deploy/crds/aerospike.com_aerospikeclusters_crd.yaml
sleep 2

kubectl apply -f deploy/rbac_operator.yaml
sleep 2

kubectl apply -f deploy/operator.yaml
sleep 10
