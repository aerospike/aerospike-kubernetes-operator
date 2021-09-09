---
title: Data On SSD
description: Data On SSD
---

Here we provide namespace storage configuration for storing namespace data on a provisioned SSD storage device.

For more details, visit [configuration of SSD Storage Engine](https://docs.aerospike.com/docs/configure/namespace/storage/#recipe-for-an-ssd-storage-engine).

## Create the namespace configuration
Following is the Storage specific config for aerospike cluster CR file.

```yaml
  storage:
    volumes:
      - storageClass: ssd
        path: /opt/aerospike
        volumeMode: filesystem
        sizeInGB: 1
      - path: /test/dev/xvdf
        storageClass: ssd
        volumeMode: block
        sizeInGB: 5
 .
 .
 .
  aerospikeConfig:
    service:
      feature-key-file: /etc/aerospike/secret/features.conf
    security:
      enable-security: true
    namespaces:
      - name: test
        memory-size: 3000000000
        replication-factor: 2
        storage-engine:
          type: device
          devices:
            - /test/dev/xvdf
```
Get full CR file [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/ssd_storage_cluster_cr.yaml).

## Deploy the cluster
Follow the instructions [here](Create-Aerospike-cluster.md#deploy-aerospike-cluster) to deploy this configuration.
