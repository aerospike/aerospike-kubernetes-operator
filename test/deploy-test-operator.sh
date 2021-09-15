#!/bin/bash

####################################
# Should be run from repository root
####################################

# Use the input operator image for testing if provided
BUNDLE_IMG=$1

# Install cert manager
kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.3.1/cert-manager.yaml
sleep 10

# Create storage classes.
kubectl apply -f config/samples/storage/gce_ssd_storage_class.yaml

if ! operator-sdk olm status; then
  operator-sdk olm install
fi

kubectl create namespace test

operator-sdk run bundle "$BUNDLE_IMG" --namespace=test
sleep 10
