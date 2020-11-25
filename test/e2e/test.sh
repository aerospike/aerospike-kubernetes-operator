#!/bin/bash

####################################
# Should be run from reposiroty root
####################################

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Cleanup
echo "Removing up operator residual k8s resources...."
$DIR/cleanup-test-namespace.sh

# Setup the deploy-test-operator.sh
echo "Deploying the operator...."
$DIR/deploy-test-operator.sh $1


operator-sdk test local ./test/e2e --no-setup --namespace test --go-test-flags "-v -timeout=59m -tags test" --kubeconfig ~/.kube/config
