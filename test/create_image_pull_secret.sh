#!/bin/bash
set -e

while getopts "n:p:" opt
do
   case "$opt" in
      n ) NAME="$OPTARG" ;;
      p ) PATH="$OPTARG" ;;
   esac
done

kubectl create secret generic "$NAME" --from-file=.dockerconfigjson="$PATH" --type=kubernetes.io/dockerconfigjson -n test