---
title: Shadow Device
description: Shadow Device
---

## Description
This is specific to cloud environments. Here namespace storage-engine can be configured to use extremely high-performance cloud instance attached local SSDs that are ephemeral. Writes will also be duplicated to another network-attached shadow device for persistence in case the cloud instance terminates.

For more details, visit [configuration of shadow devices](https://docs.aerospike.com/docs/configure/namespace/storage/#recipe-for-shadow-device).


## Create a local provisioner and local storage class
Follow the instructions [here](Storage-provisioning.md#local-volume) to create a local volume provisioner and appropriate storage class.

## Create the namespace configuration
Storage specific config for aerospike cluster CR file.
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
      - path: /dev/nvme0n1
        storageClass: local-ssd
        volumeMode: block
        sizeInGB: 5
      - path: /dev/sdf
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
            - /dev/nvme0n1 /dev/sdf
```
Get full CR file [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/shadow_device_cluster_cr.yaml).

## Deploy the cluster
Follow the instructions [here](Create-Aerospike-cluster.md#deploy-aerospike-cluster) to deploy this configuration.
