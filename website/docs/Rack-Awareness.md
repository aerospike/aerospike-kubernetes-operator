---
title: Rack Awareness
description: Rack Awareness
---

In many cloud environments, it is a best practice to build a cluster which spans multiple availability zones. Aerospike’s Rack Awareness is a good fit when you need to split the database across racks or zones. For example, if you configure a replication-factor of 2, the master copy of the partition and its replica will be stored on separate hardware failure groups. Rack Awareness provides a mechanism that allows database clients to read on a preferential basis from servers in their closely rack or zone, which can significantly reduce traffic charges by limiting cross-AZ traffic as well as provide lower latency and increased stability.

For more details, visit [Rack Awareness](https://docs.aerospike.com/docs/architecture/rack-aware.md).

## To add Rack Awareness

Rack specific config for the Aerospike cluster CR file.

```yaml
  rackConfig:
    namespaces:
      - test
    racks:
      - id: 1
        zone: us-central1-b
        aerospikeConfig:
          service:
            proto-fd-max: 18000
        storage:
          filesystemVolumePolicy:
            initMethod: deleteFiles
            cascadeDelete: true
          blockVolumePolicy:
            cascadeDelete: true
          volumes:
            - storageClass: ssd
              path: /opt/aerospike
              volumeMode: filesystem
              sizeInGB: 1
            - path: /opt/aerospike/data
              storageClass: ssd
              volumeMode: filesystem
              sizeInGB: 3
      - id: 2
        zone: us-central1-a
        aerospikeConfig:
          service:
            proto-fd-max: 16000
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
        replication-factor: 1
        storage-engine:
          type: device
          files:
            - /opt/aerospike/data/test.dat
          filesize: 2000000000
          data-in-memory: true
      - name: testMem
        memory-size: 3000000000
        replication-factor: 1
        storage-engine:
          type: memory
```

Get full CR file [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/rack_enabled_cluster_cr.yaml).

## Deploy the cluster

Follow the instructions [here](Create-Aerospike-cluster.md#deploy-aerospike-cluster) to deploy this configuration.

## Cluster node distribution in racks

Cluster nodes are distributed across racks as evenly as possible. The cluster size is divided by the number of racks to get nodes per rack. If there are remainder nodes, they are distributed one by one across racks starting from first rack.

For e.g.

Nodes: 10, Racks: 4

Topology:

- NodesForRack1: 3
- NodesForRack2: 3
- NodesForRack3: 2
- NodesForRack4: 2

## Adding a new rack in config

Add a new rack section in config yaml file under `rackConfig.racks` section and apply config file using kubectl.

```yaml
  rackConfig:
    namespaces:
      - test
    racks:
      .
      .
      .
      - id: 3
        zone: us-central1-c
```

Operator redistribute cluster nodes across racks whenever cluster size is updated or number or racks is changed. If user adds a rack without increasing cluster size then old racks will be scaled down and new new rack will be scaled up based on new nodes redistribution.

## Setting rack lavel storage and aerospikeConfig

Rack also provide for setting local storage and aerospikeConfig. If local storage is given for rack then rack will use this storage otherwise common global storage will be used. Here aerospikeConfig is config patch which will be merged with common global aerospikeConfig and will be used for rack.

```yaml
  rackConfig:
    namespaces:
      - test
    racks:
      - id: 1
        zone: us-central1-b
        aerospikeConfig:
          service:
            proto-fd-max: 18000
        storage:
          filesystemVolumePolicy:
            initMethod: deleteFiles
            cascadeDelete: true
          blockVolumePolicy:
            cascadeDelete: true
          volumes:
            - storageClass: ssd
              path: /opt/aerospike
              volumeMode: filesystem
              sizeInGB: 1
            - path: /opt/aerospike/data
              storageClass: ssd
              volumeMode: filesystem
              sizeInGB: 3
```

## Merging AerospikeConfig

Local rack AerospikeConfig patch will be merged with common global base AerospikeConfig using given rules.

- New elements from the patch configMap then it will be added to base configMap
- Base element will be replaced with a new patch element if
  - Element value type is changed
  - Element value is a primitive type and updated
  - Element value is primitive list type and updated
  - Element key is `storage-engine` and its storage-engine type has been changed. (storage-engine can be of `device`, `file`, and `memory` type.
- If element is of map type then patch and base elements will be recursively merged
- If elements are list of maps then new list elements in the patch list will be appended to the base list and corresponding entries will be merged using the same merge algorithm. Here the order of elements in the base list will be maintained. (corresponding entries are found by matching the special `name` key in maps. Here this list of maps is actually a map of map and main map keys are added in sub-map with key as `name` to convert map of maps to a list of maps).

e.g.

Rack local aerospikeConfig and common global aerospikeConfig

```yaml
  rackConfig:
    racks:
        aerospikeConfig:
          service:
            proto-fd-max: 18000
          namespaces:
            - name: test
              storage-engine:
                type: device
                devices:
                  - /dev/nvme0n2 /dev/sdf2
            - name: bar
              memory-size: 6000000000
              storage-engine:
                type: memory
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
      - name: bar
        memory-size: 3000000000
        replication-factor: 2
        storage-engine:
          type: device
          devices:
            - /dev/nvme0n10 /dev/sdf10
```

After merging rack local aerospikeConfig

```yaml
  aerospikeConfig:
    service:
      proto-fd-max: 18000
      feature-key-file: /etc/aerospike/secret/features.conf
    security:
      enable-security: true
    namespaces:
      - name: test
        memory-size: 3000000000
        replication-factor: 2
        # storage-engine type is not changed hence its merged recursively
        storage-engine:
          type: device
          devices:
            - /dev/nvme0n2 /dev/sdf2
      - name: bar
        memory-size: 6000000000
        replication-factor: 2
        # storage-engine type is changed hence its replaced
        storage-engine:
          type: memory
```

## Removing rack from config

Remove the desired rack section in config yaml file under `rackConfig.racks` section and apply config file using kubectl.

This will try to scale down the desired rack to size 0. One node at a time will be removed from rack. After removing all the nodes from the rack, rack will also be removed. If user is removing rack without decreasing cluster size then other racks will be scaled up based on new node redistribution.

## Simultaneously add and remove rack

If operator has to scale up some racks and scale down some other racks in single call then operator will always scale up first then it will scale down the racks. Hence for a short duration actual cluster size may be more than desired cluster size.
