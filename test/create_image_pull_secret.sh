#!/bin/bash
set -e

while getopts "n:p:t:" opt
do
   case "$opt" in
      n ) NAME="$OPTARG" ;;
      p ) CRED_PATH="$OPTARG" ;;
      t ) JFROG_TOKEN="$OPTARG" ;;
   esac
done

kubectl create secret generic "$NAME" --from-file=.dockerconfigjson="$CRED_PATH" --type=kubernetes.io/dockerconfigjson -n test --dry-run=client -o yaml | kubectl apply -f -

namespaces="test test1 test2 aerospike"
for namespace in $namespaces; do
  kubectl create secret docker-registry jfrogcred --docker-server=aerospike.jfrog.io --docker-username=tjain@aerospike.com --docker-password="$JFROG_TOKEN" --docker-email=tjain@aerospike.com -n $namespace
done
