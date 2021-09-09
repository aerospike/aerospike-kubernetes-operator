---
title: HDD Storage With Data In Memory
description: HDD Storage With Data In Memory
---

Here we provide namespace storage configuration for storing namespace data both in memory and on the persistent device as well.

For more details, visit [HDD Storage Engine with Data in Memory](https://docs.aerospike.com/docs/configure/namespace/storage/#recipe-for-an-hdd-storage-engine-with-data-in-memory)

## Create the namespace configuration
Following is the storage-specific config for the Aerospike cluster CR file.
```yaml
  storage:
    volumes:
      - storageClass: ssd
        path: /opt/aerospike
        volumeMode: filesystem
        sizeInGB: 1
      - path: /opt/aerospike/data
        storageClass: ssd
        volumeMode: filesystem
        sizeInGB: 3
  .
  .
  .
  aerospikeConfig:
    service:
      feature-key-file: /etc/aerospike/secret/features.conf
    security:
      enable-security: true
    namespace:
      - name: test
        memory-size: 3000000000
        replication-factor: 2
        storage-engine:
          type: device
          file:
            - /opt/aerospike/data/test.dat
          filesize: 2000000000
          data-in-memory: true
```
Get full CR file [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/hdd_dim_storage_cluster_cr.yaml).

## Deploy the cluster
Follow the instructions [here](Create-Aerospike-cluster.md#deploy-aerospike-cluster) to deploy this configuration.
