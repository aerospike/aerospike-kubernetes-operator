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

while getopts "c:f:a:r:s:" opt
do
   case "$opt" in
      c ) CONTAINER="$OPTARG" ;;
      f ) focus="$OPTARG" ;;
      a ) args="$OPTARG" ;;
      r ) registry="$OPTARG" ;;
      s ) secret="$OPTARG" ;;
   esac
done

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

go get -d github.com/onsi/ginkgo/ginkgo
go get -d github.com/onsi/gomega/...

# Cleanup
echo "---------------------------------------"
echo "| Removing residual k8s resources.... |"
echo "---------------------------------------"
"$DIR"/cleanup-test-namespace.sh || true

# Setup the deploy-test-operator.sh
echo "------------------------------"
echo "| Deploying the operator.... |"
echo "------------------------------"
"$DIR"/deploy-test-operator.sh "$CONTAINER"

# Run tests
echo "---------------------"
echo "| Starting tests.... |"
echo "---------------------"

export CUSTOM_INIT_REGISTRY="$registry"
export IMAGE_PULL_SECRET_NAME="$secret"

make test FOCUS="$focus" ARGS="$args"
