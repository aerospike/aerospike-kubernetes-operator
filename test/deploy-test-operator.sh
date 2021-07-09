#!/bin/bash

####################################
# Should be run from reposiroty root
####################################

# Use the input operator image for testing if provided
IMAGE=$1

# Install cert manager
kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.3.1/cert-manager.yaml
sleep 10

# Create storage classes.
kubectl apply -f config/samples/storage/gce_ssd_storage_class.yaml

make test-deploy  IMG="${IMAGE}" NS="test"
sleep 10
