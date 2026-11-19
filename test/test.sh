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
INIT_IMAGE_NAME_TAG=${INIT_IMAGE_NAME_TAG:-aerospike-kubernetes-init:2.5.5}


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
# imagePullSecret for the custom init image test. deploy-test-operator.sh
# creates it in every test namespace.
IMAGE_PULL_SECRET="registrycred"

"$DIR"/deploy-test-operator.sh "$BUNDLE" "${OPERATOR_PULL_CRED_PATH:-}" "$CRED_PATH" "$IMAGE_PULL_SECRET"

# Deploy LDAP
echo "------------------------------"
echo "| Deploying OpenLDAP....     |"
echo "------------------------------"
"$DIR"/deploy-openldap.sh

# Run tests
echo "---------------------"
echo "| Starting tests.... |"
echo "---------------------"

export CUSTOM_INIT_REGISTRY="$REGISTRY"
export CUSTOM_INIT_REGISTRY_NAMESPACE="$REGISTRY_NAMESPACE"
export CUSTOM_INIT_NAME_TAG="$INIT_IMAGE_NAME_TAG"
export IMAGE_PULL_SECRET_NAME="$IMAGE_PULL_SECRET"

# With -s, deploy-test-operator.sh creates ako-image-pull in every test
# namespace. The suite adds it to the Aerospike pods' service account.
if [ -n "${OPERATOR_PULL_CRED_PATH:-}" ]; then
  export SA_IMAGE_PULL_SECRET_NAME="ako-image-pull"
fi

if [ "$TEST_TYPE" = "cluster-test" ]; then
   make cluster-test FOCUS="$FOCUS" ARGS="$ARGS"
elif [ "$TEST_TYPE" = "backup-test" ]; then
   # -k runs every suite even if an earlier one fails, and still exits non-zero.
   make -k backup-service-test backup-test restore-test ARGS="$ARGS"
elif [ "$TEST_TYPE" = "all-test" ]; then
   make -k all-test FOCUS="$FOCUS" ARGS="$ARGS"
fi
