---
title: Cluster Configuration Settings
description: Cluster Configuration Settings
---

## Custom Resource Definition and Custom Resource

The Operator [CRD](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/crds/aerospike.com_aerospikeclusters_crd.yaml) specifies the CR that the operator uses. The Aerospike cluster Custom Resource (CR) based on this CRD drives the deployment and management of Aerospike clusters. To create and deploy an Aerospike cluster, create a CR yaml file.

This custom resource can be edited later on to make any changes to the Aerospike cluster.

## Example CR
A sample AerospikeCluster resource yaml file that sets up a persistent namespace and an in-memory namespace is below.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  size: 2
  image: aerospike/aerospike-server-enterprise:5.5.0.3

  multiPodPerHost: true

  storage:
    filesystemVolumePolicy:
      cascadeDelete: true
      initMethod: deleteFiles
    volumes:
      - storageClass: ssd
        path: /opt/aerospike
        volumeMode: filesystem
        sizeInGB: 1
      - path: /opt/aerospike/data
        storageClass: ssd
        volumeMode: filesystem
        sizeInGB: 3

  aerospikeConfigSecret:
    secretName: aerospike-secret
    mountPath:  /etc/aerospike/secret

  aerospikeAccessControl:
    users:
      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin

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

  rackConfig:
    namespaces:
      - test
    racks:
      - id: 1
        # Change to the zone for your k8s cluster.
        zone: us-west1-a
        # nodeName: kubernetes-minion-group-qp3m
        aerospikeConfig:
          service:
            proto-fd-max: 18000
        # rack level storage, used by pods of this rack
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
        # Change to the zone for your k8s cluster.
        zone: us-west1-a
        # nodeName: kubernetes-minion-group-tft3
        aerospikeConfig:
          service:
            proto-fd-max: 16000

  resources:
    requests:
      memory: 2Gi
      cpu: 200m

  podSpec:
    sidecars:
      - name: aerospike-prometheus-exporter
        image: "aerospike/aerospike-prometheus-exporter:1.1.6"
        ports:
        - containerPort: 9145
          name: exporter
```

Other sample Aerospike Cluster CR objects can be found [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples)

## Configuration

The initial part of the CR selects the CRD and the namespace to use for the Aerospike cluster.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike
```

## Spec

The spec section provides the configuration for the cluster.

The fields are described below

| Field                                                                    | Required | Type      | Default | Description                                                                                                                                                                                                                                                                                                         |
| ------------------------------------------------------------------------ | -------- | --------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| size <br /><sub>`Dynamic`</sub>                                          | Yes      | Integer   |         | The size/number of Aerospike node pods to run for this cluster.                                                                                                                                                                                                                                                     |
| image <br /><sub>`Dynamic` **`Rolling restart`**</sub>                   | Yes      | String    |         | The official Aerospike Enterprise Server docker image to use for the node in the cluster.                                                                                                                                                                                                                           |
| resources <br /><sub>`Dynamic` **`Rolling restart`**</sub>               | Yes      | Structure |         | Configures the memory and CPU to use for the Aerospike server container.                                                                                                                                                                                                                                            |
| validationPolicy  <br /><sub>`Dynamic`</sub>                             | No       | Structure |         | Configures the custom resource validation. See [Validation Policy](Cluster-configuration-settings.md#validation-policy) for details.                                                                                                                                                                                |
| aerospikeNetworkPolicy  <br /><sub>`Dynamic` **`Rolling restart`**</sub> | No       | Structure |         | Configures IP and port types used for access. See [Network Policy](Cluster-configuration-settings.md#network-policy) for details.                                                                                                                                                                                   |
| storage   <br /><sub>`Dynamic`</sub>                                     | No       | Structure |         | Required for persistent namespaces and for Aerospike [work directory](https://docs.aerospike.com/docs/configuration/index.md?show-removed=1#work-directory), unless the validation policy skips validating persistence of the work directory. See [Storage](Cluster-configuration-settings.md#storage) for details. |
| multiPodPerHost                                                          | No       | Boolean   | False   | Indicates if this configuration should run multiple pods per Kubernetes cluster host.                                                                                                                                                                                                                               |
| aerospikeConfigSecret <br /><sub>`Dynamic` **`Rolling restart`**</sub>   | No       | Structure |         | The names of the Kubernetes secret containing files containing sensitive data like licenses, credentials, and certificates.See [Aerospike Config Secret](Cluster-configuration-settings.md#aerospike-config-secret) for details.                                                                                    |
| aerospikeAccessControl  <br /><sub>`Dynamic`</sub>                       | No       | Structure |         | Required if Aerospike security is enabled. See [Access Control](Cluster-configuration-settings.md#aerospike-access-control) for  details                                                                                                                                                                            |
| aerospikeConfig       <br /><sub>`Dynamic` **`Rolling restart`**</sub>   | Yes      | configMap |         | A free form configMap confirming to the configuration schema for the deployed Aerospike server version. See [Aerospike Config](Cluster-configuration-settings.md#aerospike-config) for details.                                                                                                                     |
| rackConfig            <br /><sub>`Dynamic`</sub>                         | No       | Structure |         | Configures the operator to deploy rack aware Aerospike cluster. Pods will be deployed in given racks based on given configuration. See [Rack Config](Cluster-configuration-settings.md#rack-config) for details.                                                                                                    |
| podSpec            <br /><sub>`Dynamic` **`Rolling restart`**</sub>      | No       | Structure |         | Configures the Kubernetes pod running Aerospike server. See [Pod Spec](Cluster-configuration-settings.md#pod-spec) for details.                                                                                                                                                                                     |

## Validation Policy

This section configures the policy for validating the cluster CR.

The fields in this structure are

| Field                                              | Required | Type    | Default | Description                                                                                  |
| -------------------------------------------------- | -------- | ------- | ------- | -------------------------------------------------------------------------------------------- |
| skipWorkDirValidate     <br /><sub>`Dynamic`</sub> | No       | Boolean | false   | If true skips validating that the Aerospike work directory is stored on a persistent volume. |
| skipXdrDlogFileValidate <br /><sub>`Dynamic`</sub> | No       | Boolean | false   | If true skips validating that the XDR digest log is stored on a persistent volume.           |

## Network Policy

This section configures IP and port types used for access, alternate access, TLS access, and TLS alternate access endpoints on the Aerospike cluster.

Three types of endpoint configurations are supported.

- pod - uses the Kubernetes pod IP and Aerospike port that will work from other pods in the same Kubernetes cluster
- hostInternal - uses the Kubernetes cluster node's host IP and a mapped Aerospike port that will work from the VPC or internal network used by the Kubernetes cluster.
- hostExternal - uses the Kubernetes cluster node's host external/public IP and a mapped Aerospike port that should work even from outside the Kubernetes network.

The fields in this structure are

| Field                                                                   | Required | Type                                   | Default      | Description                                     |
| ----------------------------------------------------------------------- | -------- | -------------------------------------- | ------------ | ----------------------------------------------- |
| access     <br /><sub>`Dynamic` **`Rolling restart`**</sub>             | No       | Enum [pod, hostInternal, hostExternal] | hostInternal | Configures Aerospike access endpoint.           |
| alternateAccess     <br /><sub>`Dynamic` **`Rolling restart`**</sub>    | No       | Enum [pod, hostInternal, hostExternal] | hostExternal | Configures Aerospike alternate access endpoint. |
| tlsAccess     <br /><sub>`Dynamic` **`Rolling restart`**</sub>          | No       | Enum [pod, hostInternal, hostExternal] | hostInternal | Configures Aerospike TLS access endpoint.       |
| tlsAlternateAccess     <br /><sub>`Dynamic` **`Rolling restart`**</sub> | No       | Enum [pod, hostInternal, hostExternal] | hostExternal | Configures Aerospike TLS alternate endpoint.    |

## Storage

The storage section configures persistent volumes devices to provision and attach to the Aerospike cluster node container.

This section is required by default for persisting the Aerospike [work directory](https://docs.aerospike.com/docs/configuration/index.md?show-removed=1#work-directory). The working directory should be stored on persistent storage to ensure pod restarts do not reset Aerospike server metadata files.

This section is also required for persisting Aerospike namespaces.

The fields in this structure are described below.

| Field                                                 | Required | Type              | Default | Description                                                                                                                               |
| ----------------------------------------------------- | -------- | ----------------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| filesystemVolumePolicy     <br /><sub>`Dynamic`</sub> | No       | Structure         |         | [Volume policy](Cluster-configuration-settings.md#volume-policy) for filesystem volumes                                                   |
| blockVolumePolicy          <br /><sub>`Dynamic`</sub> | No       | Structure         |         | [Volume policy](Cluster-configuration-settings.md#volume-policy) for block volumes                                                        |
| Volumes                    <br /><sub>`Dynamic`</sub> | No       | List of Structure |         | List of [Volumes](Cluster-configuration-settings.md#volume) to attach to Aerospike pods. Cannot add or remove storage volumes dynamically |

### Volume Policy

Specifies persistent volumes policy to determine how new volumes are initialized.

The fields are

| Field                                    | Required | Type    | Default | Description                                                                                                                                                         |
| ---------------------------------------- | -------- | ------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| initMethod    <br /><sub>`Dynamic`</sub> | No       | Enum    | none    | Controls how the volumes are initialized when the persistent volume is attached the first time to a pod. Valid values are `none`, `dd`, `blkdiscard`, `deleteFiles` |
| cascadeDelete <br /><sub>`Dynamic`</sub> | No       | Boolean | false   | CascadeDelete determines if the persistent volumes are deleted after the pods these volumes binds to are terminated and removed from the cluster                    |

For filesystem volumes, initMethod can be `none` or `deleteFiles`.
For block volumes, initMethod can be `none`, `dd` or `blkdiscard`.

Note: `blkdiscard` will only work for devices that support [TRIM](https://en.wikipedia.org/wiki/Trim_%28computing%29). For AWS please refer to the [storage volumes guide](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/InstanceStorage.html#instance-store-volumes) to check TRIM support. If trim is not supported please use the slower `dd` in the case your devices need initialization. For other devices please verify the support for TRIM command.

### Volume

Describes a persistent volume to be attached to Aerospike devices.

The fields are

| Field                                     | Required | Type                                | Default | Description                                                                                                                                                                                                                                                                                                                             |
| ----------------------------------------- | -------- | ----------------------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| path                                      | Yes      | String                              |         | The path on the pod where this block volume or filesystem volume will be attached. For block volumes, this will be the device path. For filesystem volumes, this will be the mount point.                                                                                                                                               |
| storageClass                              | Yes      | String                              |         | The name of the storage class to use.                                                                                                                                                                                                                                                                                                   |
| volumeMode                                | Yes      | Enum - filesystem, block, configMap |         | Specified the mode this volume should be created with. Filesystem mode creates a pre-formatted filesystem and mounts it at the specified path. Block mode creates a raw device and attaches it to the device path specified above. Config Map mode mounts a [ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/)). |
| sizeInGB                                  | Yes      | Integer                             |         | The size in GB (gigabytes) to provision for this device.                                                                                                                                                                                                                                                                                |
| configMap                                 |          | String                              |         | Name of the configmap for `configmap` mode volumes. <br /><sub>`Required for configMap mode`</sub>                                                                                                                                                                                                                                      |
| initMethod <br /><sub>`Dynamic`</sub>     | No       | Enum                                | none    | Controls how this volume is initialized when the persistent volume is attached the first time to a pod. Valid values are `none`, `dd`, `blkdiscard`, `deleteFiles`                                                                                                                                                                      |
| cascadeDelete  <br /><sub>`Dynamic`</sub> | No       | Boolean                             | false   | CascadeDelete determines if the persistent volume is deleted after the pod this volume binds to is terminated and removed from the cluster                                                                                                                                                                                              |

For filesystem volumes, initMethod can be `none` or `deleteFiles`.
For block volumes, initMethod can be `none`, `dd` or `blkdiscard`.

Note: `blkdiscard` will only work for devices that support [TRIM](https://en.wikipedia.org/wiki/Trim_%28computing%29). For AWS please refer to the [storage volumes guide](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/InstanceStorage.html#instance-store-volumes) to check TRIM support. If trim is not supported please use the slower `dd` in the case your devices need initialization. For other devices please verify the support for TRIM command.

## Aerospike Access Control

Provides Aerospike [access control](https://docs.aerospike.com/docs/configure/security/access-control/index.md) configuration for the Aerospike cluster.

| Field                             | Required | Type               | Default | Description                                                                                                                                           |
| --------------------------------- | -------- | ------------------ | ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| roles  <br /><sub>`Dynamic`</sub> | No       | List of Structures |         | A list of [Role](Cluster-configuration-settings.md#aerospike-role) structures with an entry for each role.                                            |
| users  <br /><sub>`Dynamic`</sub> | No       | List of Structures |         | A list of [User](Cluster-configuration-settings.md#aerospike-user) structures with an entry for each user. Required if Aerospike security is enabled. |

If the Aerospike cluster has security enabled an entry for the "admin" user having at least "sys-admin" and "user-admin" roles is mandatory.

### Aerospike Role

Configures roles to have in the Aerospike cluster.

| Field                                      | Required | Type            | Default | Description                        |
| ------------------------------------------ | -------- | --------------- | ------- | ---------------------------------- |
| name                                       | Yes      | Strings         |         | The name of this role.             |
| privileges      <br /><sub>`Dynamic`</sub> | Yes      | List of Strings |         | The privileges to grant this role. |

### Aerospike User

Configures users to have for the aerospike cluster.

| Field                                       | Required | Type            | Default | Description                                             |
| ------------------------------------------- | -------- | --------------- | ------- | ------------------------------------------------------- |
| name                                        | Yes      | Strings         |         | The name of this user.                                  |
| secretName       <br /><sub>`Dynamic`</sub> | Yes      | String          |         | The name of the secret containing this user's password. |
| roles            <br /><sub>`Dynamic`</sub> | Yes      | List of Strings |         | The roles to grant to this user.                        |

## Aerospike Config Secret

Configures the name of the secret to use and the mount path to mount the secret files on the container.

| Field                                  | Required | Type   | Default | Description                                                       |
| -------------------------------------- | -------- | ------ | ------- | ----------------------------------------------------------------- |
| secretName  <br /><sub>`Dynamic`</sub> | Yes      | String |         | The name of the secret                                            |
| mountPath  <br /><sub>`Dynamic`</sub>  | Yes      | String |         | The path where the secret files will be mounted in the container. |

## Aerospike Config

The YAML form of Aerospike server configuration.
See [Aerospike Configuration](Aerospike-configuration-mapping.md) for details.

## Rack Config

Configures the operator to deploy rack aware Aerospike cluster. Pods will be deployed in given racks based on the given configuration.

| Field                                                       | Required | Type               | Default | Description                                                          |
| ----------------------------------------------------------- | -------- | ------------------ | ------- | -------------------------------------------------------------------- |
| namespaces <br /><sub>`Dynamic` **`Rolling restart`**</sub> | No       | List of Strings    |         | List of Aerospike namespaces for which rack feature will be enabled. |
| racks <br /><sub>`Dynamic`</sub>                            | Yes      | List of structures |         | List of [racks](Cluster-configuration-settings.md#rack)              |

See [Rack awareness](Rack-Awareness.md) for details.

### Rack

Rack specifies single rack config

| Field                                                             | Required | Type      | Default | Description                                                                                                                                                                                                                                                                                                                                               |
| ----------------------------------------------------------------- | -------- | --------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| id                                                                | Yes      | Integer   |         | Identifier for the rack.                                                                                                                                                                                                                                                                                                                                  |
| zone                                                              | No       | String    |         | Zone name for setting rack affinity. Rack pods will be deployed to the given Zone.                                                                                                                                                                                                                                                                        |
| region                                                            | No       | String    |         | Region name for setting rack affinity. Rack pods will be deployed to the given Region.                                                                                                                                                                                                                                                                    |
| rackLabel                                                         | No       | String    |         | Rack label for setting rack affinity. Rack pods will be deployed in k8s nodes having rack label `aerospike.com/rack-label: <rack-label>`.                                                                                                                                                                                                                 |
| nodeName                                                          | No       | String    |         | K8s Node name for setting rack affinity. Rack pods will be deployed on the given k8s Node.                                                                                                                                                                                                                                                                |
| aerospikeConfig  <br /><sub>`Dynamic` **`Rolling restart`**</sub> | No       | Structure |         | This local [AerospikeConfig](Cluster-configuration-settings.md#aerospike-config) is a patch, which will be merged recursively with common global AerospikeConfig and will be used for this Rack. See [merging AerospikeConfig](Rack-Awareness.md#merging-aerospikeconfig). If this AerospikeConfig is not given then global AerospikeConfig will be used. |
| storage  <br /><sub>`Dynamic`</sub>                               | No       | Structure |         | This local [Storage](Cluster-configuration-settings.md#storage) specify persistent storage to use for the pods in this rack. If this Storage is not given then global Storage will be used.                                                                                                                                                               |

## Pod Spec

Configures the Kubernetes pod running Aerospike server.
Sidecar containers for monitoring, or running connectors can be added to each Aerospike pod.

| Field    | Required | Type                                                                            | Default | Description                                                                                                     |
| -------- | -------- | ------------------------------------------------------------------------------- | ------- | --------------------------------------------------------------------------------------------------------------- |
| sidecars | No       | List of [Container](https://pkg.go.dev/k8s.io/api/core/v1#Container) structures |         | List of side containers to run along with the main Aerospike server container. Volume mounts are not supported. |

All config map volumes are attached to all sidecar containers.
Other storage volume types are not supported for sidecar containers.

See [Monitoring](Monitoring.md) for details.

## Next

- [Scale up/down](Scaling.md)
- [Aerospike version upgrade/downgrade](Version-upgrade.md)
- [Aerospike configuration change](Aerospike-configuration-change.md)
