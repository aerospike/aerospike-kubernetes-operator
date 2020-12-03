#!/bin/bash

####################################
# Should be run from reposiroty root
####################################

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Cleanup
echo "Removing residual k8s resources...."
$DIR/cleanup-test-namespace.sh

# Setup the deploy-test-operator.sh
echo "Deploying the operator...."
$DIR/deploy-test-operator.sh $1


TEST_REPORTS_DIR="build/test-results"
mkdir -p $TEST_REPORTS_DIR
GO_TEST_OUTPUT="$TEST_REPORTS_DIR/go-test.txt"
JUNIT_TEST_OUTPUT="$TEST_REPORTS_DIR/report.xml"
rm -f $GO_TEST_OUTPUT || true
rm -f $JUNIT_TEST_OUTPUT || true

# Fecth go to junit report convertor
go get -u github.com/jstemmer/go-junit-report

# Generate junit report even on test failure.
function generate_junit_report()
{
    cat $GO_TEST_OUTPUT | $GOPATH/bin/go-junit-report   > "$JUNIT_TEST_OUTPUT"
}
trap generate_junit_report EXIT

operator-sdk test local ./test/e2e --no-setup --namespace test --go-test-flags "-v -timeout=5h -tags test" --kubeconfig ~/.kube/config  2>&1 | tee "$GO_TEST_OUTPUT"

exit ${PIPESTATUS[0]}
