---
title: Version Upgrade
description: Version Upgrade
---

The Operator performs a rolling upgrade of the Aerospike cluster one node at a time.  A new node is created with the same pod configuration as the chosen node to upgrade. 

For this example assume that cluster is deployed using a file named `aerospike-cluster.yaml`.

## Initiate the Upgrade

To upgrade the Aerospike cluster change the `spec.image` field in the aerocluster CR to the desired Aerospike enterprise server docker image.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike
spec:
  size: 2
  image: aerospike/aerospike-server-enterprise:4.7.0.10
  .
  .
```

## Apply the change
```sh
$ kubectl apply -f aerospike-cluster.yaml
```

## Check the pods

The pods will undergo a rolling restart.

```sh
$ kubectl get pods -n aerospike
NAME          READY   STATUS              RESTARTS   AGE
aerocluster-0-0     1/1     Running         0          3m6s
aerocluster-0-1     1/1     Running         0          3m6s
aerocluster-0-2     1/1     Running         0          30s
aerocluster-0-3     1/1     Terminating     0          30s
```
After all the pods have restarted, use kubectl describe to get the status of the cluster. Check `image` for all Pods.

```sh
$ kubectl -n aerospike describe aerospikecluster aerocluster
Name:         aerocluster
Namespace:    aerospike
Kind:         AerospikeCluster
.
.
Status:
  .
  .
  Pods:
    aerocluster-0-0:
      Aerospike:
        Access Endpoints:
          10.128.15.225:31312
        Alternate Access Endpoints:
          34.70.193.192:31312
        Cluster Name:  aerocluster
        Node ID:       0a0
        Tls Access Endpoints:
        Tls Alternate Access Endpoints:
        Tls Name:
      Host External IP:  34.70.193.192
      Host Internal IP:  10.128.15.225
      Image:             aerospike/aerospike-server-enterprise:4.7.0.10
      Initialized Volume Paths:
        /opt/aerospike
      Pod IP:        10.0.4.6
      Pod Port:      3000
      Service Port:  31312
```
