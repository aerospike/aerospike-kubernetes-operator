#!/bin/bash

################################################
# Should be run from repository root
#
# Cleans up all resources created by test runs.
#
################################################

# Delete Aerospike clusters
echo "Removing Aerospike clusters"
kubectl -n test delete aerospikecluster --all
kubectl -n test1 delete aerospikecluster --all
kubectl -n test2 delete aerospikecluster --all
kubectl -n test3 delete aerospikecluster --all

# Force delete pods
kubectl delete pod --selector 'app=aerospike-cluster' --grace-period=0 --force --namespace test || true
kubectl delete pod --selector 'app=aerospike-cluster' --grace-period=0 --force --namespace test1 || true
kubectl delete pod --selector 'app=aerospike-cluster' --grace-period=0 --force --namespace test2 || true

# Delete PVCs
echo "Removing PVCs"
kubectl -n test delete pvc --selector 'app=aerospike-cluster' || true

# Delete the secrets
echo "Removing secrets"
kubectl -n test delete secret --selector 'app=aerospike-cluster' || true

# Delete rbac accounts and auth
echo "Removing RBAC"
kubectl delete clusterrolebinding aerospike-cluster-rolebinding || true
kubectl delete clusterrole aerospike-cluster-role || true

kubectl -n test delete serviceaccount aerospike-operator-controller-manager || true
kubectl -n test1 delete serviceaccount aerospike-operator-controller-manager || true
kubectl -n test2 delete serviceaccount aerospike-operator-controller-manager || true

# Uninstall the operator
echo "Removing test operator deployment"
operator-sdk cleanup aerospike-kubernetes-operator --namespace=test

# Delete webhook configurations. Web hooks from older versions linger around and intercept requests.
kubectl delete mutatingwebhookconfigurations.admissionregistration.k8s.io $(kubectl get  mutatingwebhookconfigurations.admissionregistration.k8s.io | grep aerospike | cut -f 1 -d " ")
kubectl delete validatingwebhookconfigurations.admissionregistration.k8s.io $(kubectl get  validatingwebhookconfigurations.admissionregistration.k8s.io | grep aerospike | cut -f 1 -d " ")

# Ensure all unlisted resources are also deleted
kubectl -n test1 delete all --all
kubectl -n test2 delete all --all
kubectl -n test delete all --all

# Delete namespaces
echo "Removing test namespaces"
kubectl delete namespace test1 || true
kubectl delete namespace test2 || true
kubectl delete namespace test || true

# Delete storage classes.
case $(kubectl get nodes -o yaml) in
  *"attachable-volumes-gce-pd"*)
    echo "Deleting ssd storage class for GKE."
    kubectl delete -f config/samples/storage/gce_ssd_storage_class.yaml
    ;;
  *"eks.amazonaws.com"*)
    echo "Deleting ssd storage class for EKS."
    kubectl delete -f config/samples/storage/eks_ssd_storage_class.yaml
    ;;
  *)
    echo "Couldn't determine cloud provider from node list. Thus couldn't Delete 'ssd' storage class."
    ;;
esac
