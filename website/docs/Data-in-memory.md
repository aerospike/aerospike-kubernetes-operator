---
title: Data In Memory
description: Data In Memory
---

Here we provide namespace storage configuration for storing namespace data in memory only.

For more details, visit [configuration of data-in-memory](https://docs.aerospike.com/docs/configure/namespace/storage/#recipe-for-data-in-memory-without-persistence).

## Create the namespace configuration
Following is the Storage specific config for aerospike cluster CR file.

```yaml
  aerospikeConfig:
    namespaces:
      - name: test
        memory-size: 3000000000
        replication-factor: 2
        storage-engine:
          type: memory
```
Get full CR file [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/dim_nostorage_cluster_cr.yaml).

## Deploy the cluster
Follow the instructions [here](Create-Aerospike-cluster.md#deploy-aerospike-cluster) to deploy this configuration.
