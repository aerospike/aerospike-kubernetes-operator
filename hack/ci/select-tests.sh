#!/bin/bash
set -euo pipefail

####################################
# Should be run from repository root
####################################

# Picks the e2e suites to run, with the same rules as the "Detect Changes"
# stage of the old Jenkinsfile.
#
#   select-tests.sh all                       # master, nightly, manual runs
#   git diff --name-only ... | select-tests.sh  # pull requests
#
# Prints:
#   suites=<JSON list of test.sh -t values>, e.g. ["cluster-test","backup-test"]
#   focus=<ginkgo focus for cluster-test>; empty runs the whole suite

if [ "${1:-}" = "all" ]; then
  echo 'suites=["cluster-test","backup-test"]'
  echo 'focus='
  exit 0
fi

cluster=false backup=false all=false smoke=false sample=false

# "|| [ -n "$f" ]" keeps a last line that has no trailing newline.
while IFS= read -r f || [ -n "$f" ]; do
  [ -z "$f" ] && continue

  # cluster related files, e.g. internal/controller/cluster/reconciler.go
  if [[ $f == *.go && ( $f == *cluster/* || $f == *v1/* ) ]]; then
    cluster=true
  fi

  # backup related files, e.g. internal/controller/backup/reconciler.go
  if [[ $f == *.go && ( $f == */backup* || $f == *restore/* || $f == *v1beta1/* ) ]]; then
    backup=true
  fi

  # common files, e.g. cmd/main.go
  if [[ $f == cmd/* ||
        ( $f == internal/controller/common/* && $f != *backup_config_util.go* ) ||
        $f == pkg/utils/* || $f == errors/* ]]; then
    all=true
  fi

  # non-code files or some specific directories, e.g. config/webhook/manifests.yaml
  if [[ ( $f == pkg/* && $f != *backup-service/* && $f != *utils/* ) ||
        ( $f == test/* && $f != *cluster/* && $f != *backup/* && $f != *restore/* && $f != *backup_service/* ) ||
        ( $f != helm-charts/* && $f != *.go && $f != *_cr.yaml ) ]]; then
    smoke=true
  fi

  # sample files, e.g. config/samples/dim_nostorage_cluster_cr.yaml
  if [[ $f == config/samples/* && $f != *openldap/* && $f != *secrets/* && $f != *storage/* && $f == *_cr.yaml ]]; then
    sample=true
  fi
done

# Same order as the Jenkins stages: "all" overrides everything, a full
# cluster run makes the smoke run unnecessary.
focus=""
if [ "$all" = true ]; then
  cluster=true
  backup=true
elif [ "$cluster" = false ] && [ "$smoke" = true ]; then
  cluster=true
  focus="Sample files validation|UpdateAerospikeCluster"
elif [ "$cluster" = false ] && [ "$sample" = true ]; then
  cluster=true
  focus="Sample files validation"
fi

suites=""
if [ "$cluster" = true ]; then
  suites='"cluster-test"'
fi
if [ "$backup" = true ]; then
  suites="${suites:+$suites,}\"backup-test\""
fi

echo "suites=[$suites]"
echo "focus=$focus"
