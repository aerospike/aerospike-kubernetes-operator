#!/usr/bin/env bash

kubectl apply -f deploy/storage_class.yaml
sleep 2

kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike
sleep 2

kubectl create secret generic auth-secret --from-literal=password='admin123' -n aerospike
sleep 2

kubectl apply -f deploy/samples/aerospike.com_v1alpha1_aerospikecluster_cr.yaml
