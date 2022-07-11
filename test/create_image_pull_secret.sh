#!/bin/bash
set -e

while getopts "n:p:" opt
do
   case "$opt" in
      n ) NAME="$OPTARG" ;;
      p ) CRED_PATH="$OPTARG" ;;
   esac
done

kubectl create secret generic "$NAME" --from-file=.dockerconfigjson="$CRED_PATH" --type=kubernetes.io/dockerconfigjson -n test --dry-run=client -o yaml | kubectl apply -f -
