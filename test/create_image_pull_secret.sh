#!/bin/bash
set -e

while getopts "n:p:N:" opt
do
   case "$opt" in
      n ) NAME="$OPTARG" ;;
      p ) CRED_PATH="$OPTARG" ;;
      N ) NAMESPACE="$OPTARG" ;;
   esac
done

NAMESPACE=${NAMESPACE:-test}

kubectl create secret generic "$NAME" --from-file=.dockerconfigjson="$CRED_PATH" --type=kubernetes.io/dockerconfigjson -n "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
