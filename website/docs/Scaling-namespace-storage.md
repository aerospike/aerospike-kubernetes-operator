---
title: Scaling Namespace Storage
description: Scaling Namespace Storage
---

Scaling namespace storage (vertical scaling) is a bit complex. The Operator uses k8s StatefulSet for deploying Aerospike-cluster. StatefulSet uses PersistentVolumeClaim for providing storage. Currently a PersistentVolumeClaim cannot be updated. Hence the Operator can not provide a simple solution for vertical scaling.

## Aerospike Rack Awareness for Vertical Scaling

To perform vertical scaling, the Aerospike Rack Awareness feature can be applied.

For this example, we assume that cluster is deployed with the name `aerospike-cluster.yaml`.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  size: 2
  image: aerospike/aerospike-server-enterprise:4.7.0.10

  rackConfig:
    namespaces:
      - test
    racks:
      - id: 1
        zone: us-central1-b
        storage:
          volumes:
            - path: /dev/sdf
              storageClass: ssd
              volumeMode: block
              sizeInGB: 5

  aerospikeConfig:
    service:
      feature-key-file: /etc/aerospike/secret/features.conf
    security:
      enable-security: true
    namespaces:
      - name: test
        memory-size: 6000000000
        replication-factor: 2
        storage-engine:
          type: device
          devices:
            - /dev/sdf
.
.
.
```

## Create a new rack

Now if we want to resize `/dev/sdf` for namespace `test` then we have to create a new `rack` inside `rackConfig` with updated `storage` config and remove the old rack.

The new rack can be created in same physical rack using existing `zone/region` (if there is enough space) to hold new storage and old storage together.

## Update the `rackConfig` section

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  size: 2
  image: aerospike/aerospike-server-enterprise:4.7.0.10

  rackConfig:
    namespaces:
      - test
    racks:
      # Added new rack with id: 2. Old rack with id: 1 is removed
      - id: 2
        zone: us-central1-b
        storage:
          volumes:
            - path: /dev/sdf
              storageClass: ssd
              volumeMode: block
              sizeInGB: 8

  aerospikeConfig:
    service:
      feature-key-file: /etc/aerospike/secret/features.conf
    security:
      enable-security: true
    namespaces:
      - name: test
        memory-size: 10000000000
        replication-factor: 2
        storage-engine:
          type: device
          devices:
            - /dev/sdf
.
.
.
```

## Apply the change
```sh
$ kubectl apply -f aerospike-cluster.yaml
```
This will create a new rack with `id: 2` and updated `storage` config. Old data will be migrated to new rack. Old rack will be removed gracefully.

## Check the pods

```sh
$ kubectl get pods -n aerospike
NAME                READY   STATUS          RESTARTS   AGE
aerocluster-2-0     1/1     Running         0          3m6s
aerocluster-2-1     1/1     Running         0          3m6s
aerocluster-1-1     1/1     Terminating     0          30s
```
