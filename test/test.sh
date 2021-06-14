#!/bin/bash

####################################
# Should be run from reposiroty root
####################################

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

go get github.com/onsi/ginkgo/ginkgo
go get github.com/onsi/gomega/...

# Cleanup
echo "Removing residual k8s resources...."
$DIR/cleanup-test-namespace.sh

# Setup the deploy-test-operator.sh
echo "Deploying the operator...."
$DIR/deploy-test-operator.sh $1

# Run tests
make test