#!/bin/bash

if [ "$1" = "run" ]
then
  make generate
  make manifests
  IMAGE_TAG_BASE=davi17g/aerospike-kubernetes-operator
  VERSION=2.0.0-5.6.0.6-dev
  make docker-build docker-push IMG=$IMAGE_TAG_BASE:$VERSION
  kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.3.1/cert-manager.yaml
  make deploy IMG=$IMAGE_TAG_BASE:$VERSION
  kubectl apply -f cluster_rbac.yaml
  kubectl apply -f config/crd/bases/asdb.aerospike.com_aerospikeclusters.yaml
  kubectl apply -f cluster_rbac.yaml
  kubectl apply -f config/samples/storage/gce_ssd_storage_class.yaml
  kubectl -n aerospike create secret generic aerospike-secret --from-file=./config/secrets
  kubectl -n aerospike create secret generic auth-secret --from-literal=password='admin123'
  echo "Log Command kubectl logs -f $(kubectl get pods -n aerospike -o name | awk -F'/' '{print $2}') -n aerospike -c manager"
fi

if [ "$1" = "build" ]
then
    make generate
    make manifests
    IMAGE_TAG_BASE=davi17g/aerospike-kubernetes-operator
    VERSION=2.0.0-5.6.0.6-dev
    make docker-build docker-push IMG=$IMAGE_TAG_BASE:$VERSION

fi
if [ "$1" = "rebuild" ]
then
  make undeploy
  make docker-build docker-push IMG=$IMAGE_TAG_BASE:$VERSION
  make deploy IMG=$IMAGE_TAG_BASE:$VERSION
fi

if [ "$1" = "clean" ]
then
  ./test/cleanup-test-namespace.sh
  kubectl delete -f aerospike-cluster.yml
  make undeploy
   kubectl delete -f https://github.com/jetstack/cert-manager/releases/download/v1.3.1/cert-manager.yaml
   kubectl delete -f cluster_rbac.yaml
   kubectl delete -f config/crd/bases/asdb.aerospike.com_aerospikeclusters.yaml
  kubectl delete -f config/samples/storage/gce_ssd_storage_class.yaml
fi

