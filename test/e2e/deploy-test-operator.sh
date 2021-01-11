#!/bin/bash

####################################
# Should be run from reposiroty root
####################################

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Use the input operator image for testing if provided
IMAGE=$1

if [[ ! -z "$IMAGE" ]]; then
    sed -i  "s@image: .*@image: $IMAGE@g" $DIR/setup_operator_test.yaml
fi

# Create the custom custom resource.
kubectl -n test apply -f deploy/crds/aerospike.com_aerospikeclusters_crd.yaml

# Create storage classes.
kubectl -n test apply -f deploy/samples/storage-classes/local-storage-class.yaml
kubectl -n test apply -f deploy/samples/storage-classes/gce-ssd-storage-class.yaml

# Create the test namespace
kubectl create namespace test || true
kubectl -n test apply -f $DIR/setup_operator_test.yaml
