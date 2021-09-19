#!/bin/bash

####################################
# Should be run from repository root
####################################

# Use the input operator image for testing if provided
BUNDLE_IMG=$1

# Create storage classes.
kubectl apply -f config/samples/storage/gce_ssd_storage_class.yaml

if ! operator-sdk olm status; then
  operator-sdk olm install
fi

kubectl create namespace test
kubectl create namespace test1
kubectl create namespace test2

namespaces="test test1 test2"
operator-sdk run bundle "$BUNDLE_IMG"  --namespace=test --install-mode MultiNamespace=$(echo "$namespaces" | tr " " ",")

for namespace in $namespaces; do
ATTEMPT=0
until [ $ATTEMPT -eq 10 ] || kubectl get csv -n $namespace | grep Succeeded; do
    sleep 2
    ((ATTEMPT+=1))
done
done
sleep 10
