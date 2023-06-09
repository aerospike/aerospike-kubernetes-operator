#!/bin/bash
set -e

####################################
# Should be run from repository root
####################################

# Usage test.sh <operator-bundle-image> [<test-args>]
# e.g.
#  test.sh -c aerospike/aerospike-kubernetes-operator-bundle:1.1.0
#  test.sh -c aerospike/aerospike-kubernetes-operator-bundle:1.1.0 -f ".*RackManagement.*" -a "--connect-through-network-type=hostInternal"
#  test.sh -c <IMAGE> -f "<GINKGO-FOCUS-REGEXP>" -a "<PASS-THROUGHS>"

while getopts "b:c:f:a:r:p:" opt
do
   case "$opt" in
      b ) BUNDLE="$OPTARG" ;;
      c ) CATALOG="$OPTARG" ;;
      f ) FOCUS="$OPTARG" ;;
      a ) ARGS="$OPTARG" ;;
      r ) REGISTRY="$OPTARG" ;;
      p ) CRED_PATH="$OPTARG" ;;

   esac
done

# Defaults
CRED_PATH=${CRED_PATH:-$HOME/.docker/config.json}
REGISTRY=${REGISTRY:-568976754000.dkr.ecr.ap-south-1.amazonaws.com}


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
"$DIR"/deploy-test-operator.sh "$BUNDLE" "$CATALOG"

# Deploy LDAP
echo "------------------------------"
echo "| Deploying OpenLDAP....     |"
echo "------------------------------"
"$DIR"/deploy-openldap.sh

# Create imagePullSecret for AerospikeInitImage
IMAGE_PULL_SECRET="registrycred"

"$DIR"/create_image_pull_secret.sh -n ${IMAGE_PULL_SECRET} -p "$CRED_PATH"

# Run tests
echo "---------------------"
echo "| Starting tests.... |"
echo "---------------------"

export CUSTOM_INIT_REGISTRY="$REGISTRY"
export IMAGE_PULL_SECRET_NAME="$IMAGE_PULL_SECRET"

make test FOCUS="$FOCUS" ARGS="$ARGS"
