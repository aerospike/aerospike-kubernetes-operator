#!/bin/bash
set -e

####################################
# Should be run from repository root
####################################

# Usage test.sh <operator-bundle-image> [<test-args>]
# e.g.
#  test.sh -b aerospike/aerospike-kubernetes-operator-bundle:1.1.0
#  test.sh -b aerospike/aerospike-kubernetes-operator-bundle:1.1.0 -f ".*RackManagement.*" -a "--connect-through-network-type=hostInternal"
#  test.sh -b <IMAGE> -f "<GINKGO-FOCUS-REGEXP>" -a "<PASS-THROUGHS>"
#  test.sh -b <IMAGE> -s <DOCKER-CONFIG-JSON>   # bundle and operator images in a private registry

while getopts "b:f:a:r:p:n:i:t:s:" opt
do
   case "$opt" in
      b ) BUNDLE="$OPTARG" ;;
      f ) FOCUS="$OPTARG" ;;
      a ) ARGS="$OPTARG" ;;
      r ) REGISTRY="$OPTARG" ;;
      p ) CRED_PATH="$OPTARG" ;;
      n ) REGISTRY_NAMESPACE="$OPTARG" ;;
      i ) INIT_IMAGE_NAME_TAG="$OPTARG" ;;
      t ) TEST_TYPE="$OPTARG" ;;
      s ) OPERATOR_PULL_CRED_PATH="$OPTARG" ;;

   esac
done

# Defaults
CRED_PATH=${CRED_PATH:-$HOME/.docker/config.json}
REGISTRY=${REGISTRY:-568976754000.dkr.ecr.ap-south-1.amazonaws.com}
REGISTRY_NAMESPACE=${REGISTRY_NAMESPACE:-aerospike}
INIT_IMAGE_NAME_TAG=${INIT_IMAGE_NAME_TAG:-aerospike-kubernetes-init:2.5.4}


DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Cleanup
echo "---------------------------------------"
echo "| Removing residual k8s resources.... |"
echo "---------------------------------------"
"$DIR"/cleanup-test-namespace.sh || true

# Setup the deploy-test-operator.sh
echo "------------------------------"
echo "| Deploying the operator.... |"
echo "------------------------------"
"$DIR"/deploy-test-operator.sh "$BUNDLE" "${OPERATOR_PULL_CRED_PATH:-}"

# Deploy LDAP
echo "------------------------------"
echo "| Deploying OpenLDAP....     |"
echo "------------------------------"
"$DIR"/deploy-openldap.sh

# Create imagePullSecret for AerospikeInitImage. The test suite adds it to the
# Aerospike pods' service account in every namespace.
IMAGE_PULL_SECRET="registrycred"

for namespace in test test1 test2 aerospike; do
  "$DIR"/create_image_pull_secret.sh -n ${IMAGE_PULL_SECRET} -p "$CRED_PATH" -N "$namespace"
done

# Run tests
echo "---------------------"
echo "| Starting tests.... |"
echo "---------------------"

export CUSTOM_INIT_REGISTRY="$REGISTRY"
export CUSTOM_INIT_REGISTRY_NAMESPACE="$REGISTRY_NAMESPACE"
export CUSTOM_INIT_NAME_TAG="$INIT_IMAGE_NAME_TAG"
export IMAGE_PULL_SECRET_NAME="$IMAGE_PULL_SECRET"

if [ "$TEST_TYPE" = "cluster-test" ]; then
   make cluster-test FOCUS="$FOCUS" ARGS="$ARGS"
elif [ "$TEST_TYPE" = "backup-test" ]; then
   make backup-service-test ARGS="$ARGS"
   make backup-test ARGS="$ARGS"
   make restore-test ARGS="$ARGS"
elif [ "$TEST_TYPE" = "all-test" ]; then
   make -k all-test FOCUS="$FOCUS" ARGS="$ARGS"
fi
