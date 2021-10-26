#!/bin/bash
set -e

####################################
# Should be run from repository root
####################################

# Use the input operator image for testing if provided
BUNDLE_IMG=$1

# Create storage classes.
case $(kubectl get nodes -o yaml) in
  *"attachable-volumes-gce-pd"*)
    echo "Installing ssd storage class for GKE."
    kubectl apply -f config/samples/storage/gce_ssd_storage_class.yaml
    ;;
  *"eks.amazonaws.com"*)
    echo "Installing ssd storage class for EKS."
    kubectl apply -f config/samples/storage/eks_ssd_storage_class.yaml
    ;;
  *)
    echo "Couldn't determine cloud provider from node list. Thus couldn't install 'ssd' storage class. Either install it manually or most likely ssd tests will fail."
    ;;
esac

IS_OPENSHIFT_CLUSTER=$(kubectl get all | grep -c openshift)

if ! $IS_OPENSHIFT_CLUSTER; then
  if ! operator-sdk olm status; then
    operator-sdk olm install
  fi
fi
kubectl create namespace test
kubectl create namespace test1
kubectl create namespace test2

if $IS_OPENSHIFT_CLUSTER; then
  oc adm policy add-scc-to-user anyuid system:serviceaccount:test:aerospike-operator-controller-manager
  oc adm policy add-scc-to-user anyuid system:serviceaccount:test1:aerospike-operator-controller-manager
  oc adm policy add-scc-to-user anyuid system:serviceaccount:test2:aerospike-operator-controller-manager

  oc adm policy add-scc-to-user privileged -z aerospike-operator-controller-manager -n test
  oc adm policy add-scc-to-user privileged -z aerospike-operator-controller-manager -n test1
  oc adm policy add-scc-to-user privileged -z aerospike-operator-controller-manager -n test2

  fi

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
