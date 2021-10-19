#!/bin/bash
set -e

####################################
# Should be run from repository root
####################################

# Use the input operator image for testing if provided
BUNDLE_IMG=$1

# Create storage classes.
case $(kubectl get nodes -o yaml) in
  *"cloud.google.com"*)
    echo "Instaling ssd storage class for GKE."
    kubectl apply -f config/samples/storage/gce_ssd_storage_class.yaml
    ;;
  *"eks.amazonaws.com"*)
    echo "Instaling ssd storage class for EKS."
    kubectl apply -f config/samples/storage/eks_ssd_storage_class.yaml
    ;;
  *)
    echo "Couldn't determine cloud provider from node list. Thus couldn't install 'ssd' storage class. Either install it manually or most likely ssd tests will fail."
    ;;
esac

if ! operator-sdk olm status; then
  operator-sdk olm install
fi

kubectl create namespace test
kubectl create namespace test1
kubectl create namespace test2

namespaces="test test1 test2"
operator-sdk run bundle "$BUNDLE_IMG"  --namespace=test --install-mode MultiNamespace=$(echo "$namespaces" | tr " " ",")

for namespace in $namespaces; do
ATTEMPT=0
until [ $ATTEMPT -eq 10 ] || kubectl get csv -n $namespace | grep Succeeded; do
    sleep 2
    ((ATTEMPT+=1))
done
done
sleep 10
