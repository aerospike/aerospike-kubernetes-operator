#!/bin/bash

################################################
# Should be run from repository root
#
# Cleans up all resources created by test runs.
#
################################################

namespaces="test test1 test2 aerospike"

for namespace in $namespaces; do
  # Delete Aerospike clusters
  echo "Removing Aerospike clusters from namespace: $namespace"
  kubectl -n "$namespace" delete aerospikecluster --all

  # Force delete pods
  kubectl -n "$namespace" delete pod --selector 'app=aerospike-cluster' --grace-period=0 --force --ignore-not-found

  # Delete PVCs
  echo "Removing PVCs from namespace: $namespace"
  kubectl -n "$namespace" delete pvc --selector 'app=aerospike-cluster' --ignore-not-found

  # Delete the secrets
  echo "Removing secrets from namespace: $namespace"
  kubectl -n "$namespace" delete secret --selector 'app=aerospike-cluster' --ignore-not-found

  echo "Removing serviceaccount from namespace: $namespace"
  kubectl -n "$namespace" delete serviceaccount aerospike-operator-controller-manager --ignore-not-found

done

# Delete rbac accounts and auth
echo "Removing RBAC"
kubectl delete clusterrolebinding aerospike-kubernetes-operator-default-ns aerospike-cluster --ignore-not-found
kubectl delete clusterrole aerospike-kubernetes-operator-default-ns aerospike-cluster --ignore-not-found

# Uninstall the operator
echo "Removing test operator deployment"
OPERATOR_NS=test
kubectl delete subscription -n $OPERATOR_NS $(kubectl get subscription -n $OPERATOR_NS | grep aerospike-kubernetes-operator | cut -f 1 -d ' ') --ignore-not-found
kubectl delete clusterserviceversion -n $OPERATOR_NS $(kubectl get clusterserviceversion -n $OPERATOR_NS | grep aerospike-kubernetes-operator | cut -f 1 -d ' ') --ignore-not-found
kubectl delete job $(kubectl get job -o=jsonpath='{.items[?(@.status.succeeded==1)].metadata.name}' -n $OPERATOR_NS) -n $OPERATOR_NS --ignore-not-found
kubectl delete CatalogSource $(kubectl get CatalogSource -n $OPERATOR_NS | grep aerospike-kubernetes-operator | cut -f 1 -d ' ') --ignore-not-found
kubectl delete crd aerospikeclusters.asdb.aerospike.com --ignore-not-found

# Delete webhook configurations. Web hooks from older versions linger around and intercept requests.
kubectl delete mutatingwebhookconfigurations.admissionregistration.k8s.io $(kubectl get mutatingwebhookconfigurations.admissionregistration.k8s.io | grep aerospike | cut -f 1 -d " ")
kubectl delete validatingwebhookconfigurations.admissionregistration.k8s.io $(kubectl get validatingwebhookconfigurations.admissionregistration.k8s.io | grep aerospike | cut -f 1 -d " ")

for namespace in $namespaces; do
  # Delete operator CSVs
  kubectl -n "$namespace" delete csv $(kubectl get csv -n "$namespace" | grep aerospike | cut -f 1 -d " ")

  # Ensure all unlisted resources are also deleted
  kubectl -n "$namespace" delete all --all

  # Delete namespaces
  echo "Removing namespace $namespace"
  kubectl delete namespace "$namespace" --ignore-not-found
done
