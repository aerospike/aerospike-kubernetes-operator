#!/bin/bash
set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

OPENLDAP_DIR="$DIR/../config/samples/openldap"

# Create the ldap secret.
kubectl delete secret openldap || true
kubectl create secret generic openldap --from-literal=adminpassword=adminpassword --from-literal=users=user01,user02 --from-literal=passwords=password01,password02

# Deploy LDAP
kubectl delete -f "$OPENLDAP_DIR/openldap-deployment.yaml" || true
kubectl apply -f "$OPENLDAP_DIR/openldap-deployment.yaml"

# Create service
kubectl apply -f "$OPENLDAP_DIR/openldap-svc.yaml"