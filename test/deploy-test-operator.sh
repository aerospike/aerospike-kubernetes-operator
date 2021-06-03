#!/bin/bash

####################################
# Should be run from reposiroty root
####################################

# Use the input operator image for testing if provided
IMAGE=$1

# Create storage classes.
kubectl -n test apply -f deploy/samples/storage/gce_ssd_storage_class.yaml

make deploy  IMG=${IMAGE}
