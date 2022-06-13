# OpenLDAP sample deployment

This is an example OpenLDAP setup using bitnami LDAP container for demo purposes only. It has been adapted from
[here](https://docs.bitnami.com/tutorials/create-openldap-server-kubernetes/).

It launches a single node LDAP server with two users

- user01 with password password01
- user02 with password password02

These users are added to read-write group.

## Deploy OpenLDAP

Create a secret listing sample users and passwords

```shell
kubectl create secret generic openldap --from-literal=adminpassword=adminpassword --from-literal=users=user01,user02 --from-literal=passwords=password01,password02
```

Deploy a single instance OpenLDAP server

```shell
kubectl apply -f openldap-deployment.yaml
```

## Create a service as the entrypoint

Create a Kubernetes service to serve as the named endpoint for the OpenLDAP deployment.

```shell
kubectl apply -f openldap-svc.yaml
```

## Verify that the service is running

Describe the service and ensure the Endpoints list is not empty

```shell
kubectl describe svc openldap
```

You should see something like this

```shell
Name:              openldap
Namespace:         default
Labels:            app.kubernetes.io/name=openldap
Annotations:       cloud.google.com/neg: {"ingress":true}
Selector:          app.kubernetes.io/name=openldap
Type:              ClusterIP
IP Family Policy:  SingleStack
IP Families:       IPv4
IP:                10.84.16.76
IPs:               10.84.16.76
Port:              tcp-ldap  1389/TCP
TargetPort:        tcp-ldap/TCP
Endpoints:         10.88.5.33:1389
Session Affinity:  None
Events:            <none>
```

## Use OpenLDAP with Aerospike

Refer to the [LDAP authentication cluster example](../ldap_cluster_cr.yaml).

## Delete OpenLDAP installation

Delete the service

```shell
kubectl delete -f openldap-svc.yaml
```

Delete the OpenLDAP deployment

```shell
kubectl delete -f openldap-deployment.yaml
```

Delete the secret

```shell
kubectl delete secret openldap
```
