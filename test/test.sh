#!/bin/bash
set -e

####################################
# Should be run from repository root
####################################

# Usage test.sh <operator-bundle-image> [<test-args>]
# e.g.
#  test.sh aerospike/aerospike-kubernetes-operator-bundle:1.1.0
#  test.sh aerospike/aerospike-kubernetes-operator-bundle:1.1.0 '-ginkgo.focus=".*RackManagement.*"'

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
"$DIR"/deploy-test-operator.sh "$1"


# Run tests
echo "---------------------"
echo "| Starting tests.... |"
echo "---------------------"
make test TEST_ARGS="$2"
