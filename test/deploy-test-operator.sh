#!/bin/bash
set -e

####################################
# Should be run from repository root
####################################

# Use the input operator image for testing if provided
BUNDLE_IMG=$1
CATALOG_IMG=$2

# Create storage classes.
case $(kubectl get nodes -o yaml) in
  *"attachable-volumes-gce-pd"*)
    echo "Installing ssd storage class for GKE."
    kubectl apply -f config/samples/storage/gce_ssd_storage_class.yaml
    ;;
  *"attachable-volumes-aws-ebs"*)
    echo "Installing ssd storage class for EKS."
    kubectl apply -f config/samples/storage/eks_ssd_storage_class.yaml
    ;;
  *)
    echo "Couldn't determine cloud provider from node list. Thus couldn't install 'ssd' storage class. Either install it manually or most likely ssd tests will fail."
    ;;
esac

IS_OPENSHIFT_CLUSTER=0
if kubectl get namespace | grep -o -a -m 1 -h openshift > /dev/null; then
  IS_OPENSHIFT_CLUSTER=1
fi

if [ $IS_OPENSHIFT_CLUSTER == 0 ]; then
  if ! operator-sdk olm status; then
    operator-sdk version
    operator-sdk olm install
  fi
fi

# NOTE: Update targetNamespaces list in custom_operator_deployment.yaml OperatorGroup Kind as well if below namespaces list is updated
namespaces="test test1 test2 aerospike"
for namespace in $namespaces; do
  kubectl create namespace "$namespace" || true
  if [ $IS_OPENSHIFT_CLUSTER == 1 ]; then
    echo "Adding security constraints"
    oc adm policy add-scc-to-user anyuid system:serviceaccount:"$namespace":aerospike-operator-controller-manager
    # TODO: Find minimum privileges that should be granted
    oc adm policy add-scc-to-user privileged -z aerospike-operator-controller-manager -n $namespace
  fi
done

sed -i "s@CATALOG_IMG@${CATALOG_IMG}@g" test/custom_operator_deployment.yaml
kubectl apply -f test/custom_operator_deployment.yaml

for namespace in $namespaces; do
  ATTEMPT=0
  until [ $ATTEMPT -eq 15 ] || kubectl get csv -n "$namespace" | grep Succeeded; do
    sleep 2
    ((ATTEMPT += 1))
  done
done
sleep 10
