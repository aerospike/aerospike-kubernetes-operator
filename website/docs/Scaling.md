---
title: Scaling
description: Scaling
---

For this example assume that cluster is deployed using a file named `aerospike-cluster.yaml`.

## Change the size
Change the `spec.size` field in the yaml file to scale up/down the cluster.

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

```sh
$ kubectl get pods -n aerospike
NAME          READY   STATUS      RESTARTS   AGE
aerocluster-0-0     1/1     Running     0          3m6s
aerocluster-0-1     1/1     Running     0          3m6s
aerocluster-0-2     1/1     Running     0          30s
aerocluster-0-3     1/1     Running     0          30s
```
