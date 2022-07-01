#!/bin/bash
set -e

while getopts "n:p:" opt
do
   case "$opt" in
      n ) NAME="$OPTARG" ;;
      p ) CREDPATH="$OPTARG" ;;
   esac
done

kubectl create secret generic "$NAME" --from-file=.dockerconfigjson="$CREDPATH" --type=kubernetes.io/dockerconfigjson -n test
