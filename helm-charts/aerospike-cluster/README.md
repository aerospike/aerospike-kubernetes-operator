# Aerospike Cluster (Custom Resource) Helm Chart

A Helm chart for `AerospikeCluster` custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.16+
- Aerospike Kubernetes Operator

## Usage

### Add Aerospike Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-operator
```

### Deploy Aerospike Cluster

Create a secret containing aerospike feature key file - `features.conf`,

```sh
kubectl create secret generic aerospike-secrets --from-file=<path-to-features.conf-file>
```

Install the chart,

```sh
helm install aerospike aerospike/aerospike-cluster \
    --set devMode=true \
    --set aerospikeSecretName=aerospike-secrets
```

*Note that this command assumes few defaults and deploys an aerospike cluster in **"dev"** mode with no data
persistence. It is recommended to create a separate YAML file with configurations as per your requirements and use it
with `helm install`.*

```sh
helm install aerospike aerospike/aerospike-cluster \
    -f <customized-values-yaml-file>`
```

## Configurations

| Name       | Description | Default   |
| ---------- | ----------- | --------- |
| `replicas` | Aerospike cluster size | `3` |
| `image.repository` | Aerospike server container image repository | `aerospike/aerospike-server-enterprise` |
| `image.tag` | Aerospike server container image tag | `5.5.0.9` |
| `imagePullSecrets` | Secrets containing credentials to pull Aerospike container image from a private registry | `{}` (nil) |
| `multiPodPerHost` | Set this to `true` to allow scheduling multiple pods per kubernetes node | `true` |
| `aerospikeAccessControl` | Aerospike access control configuration. Define users and roles to be created on the cluster. | `{}` (nil) |
| `aerospikeConfig` | Aerospike configuration | `{}` (nil) |
| `aerospikeSecretName`| Secret containing Aerospike feature key file, TLS certificates etc. | `""` |
| `aerospikeSecretMountPath` | Mount path inside for the `aerospikeSecretName` secret | `/etc/aerospike/secrets/` |
| `aerospikeNetworkPolicy` | Network policy (client access configuration) | `{}` (nil) |
| `commonName` | Base string for naming pods, services, stateful sets, etc.  | Release name truncated to 63 characters (without hyphens) |
| `podSpec` | Aerospike pod spec configuration | `{}` (nil) |
| `rackConfig` | Aerospike rack configuration | `{}` (nil) |
| `storage` | Aerospike pod storage configuration | `{}` (nil) |
| `validationPolicy` | Validation policy | `{}` (nil) |
| `resources` | Resource requests and limits for Aerospike pod | `{}` (nil) |
| `devMode` | Deploy Aerospike cluster in "dev" mode | `false` |

### Default values in "dev" mode (`devMode=true`):

The following values are set as defaults when the cluster is deployed in "dev" mode.

```yaml
aerospikeConfig:
  service:
    feature-key-file: /etc/aerospike/secrets/features.conf

  namespaces:
    - name: test
      memory-size: 1073741824
      replication-factor: 2
      storage-engine:
        type: memory

aerospikeSecretMountPath: /etc/aerospike/secrets/

validationPolicy:
  skipWorkDirValidate: true
  skipXdrDlogFileValidate: true

resources:
  requests:
    memory: 1Gi
    cpu: 100m
```

### Configurations Explained

- `aerospikeAccessControl`

  | Field | Type | Sub-type / Sub-field | Description |
          | ----- | ---- | -------- | ----------- |
  | `users` | `array` | Type `User` | List of Users |
  | `adminPolicy` | `object` | Field `timeout` (Timeout for adminPolicy in client (in milliseconds)), Type `integer` | AdminPolicy for access control operations |
  | `roles` | `array` | Type `Role` | List of roles |

    - Type `User`

      | Field | Type | Sub-type | Description |
                        | ----- | ---- | -------- | ----------- |
      | `name` | `string` |  | Name of the user |
      | `roles` | `array` | `string` | Roles for the user |
      | `secretName` | `string` | | Secret containing the password |

    - Type `Role`

      | Field | Type | Sub-type | Description |
                        | ----- | ---- | -------- | ----------- |
      | `name` | `string` |  | Name of the role |
      | `privileges` | `array` | `string` | Privileges for the role |
      | `whitelist` | `array` | `string` | Whitelist of host address allowed for the role |

  Example,
    ```yaml
    aerospikeAccessControl:
      users:
      - name: admin
        secretName: auth-secret-admin
        roles:
        - user-admin
        - sys-admin
      - name: superman
        secretName: auth-secret-superman
        roles:
        - superuser
      adminPolicy:
        timeout: 1000
      roles:
      - name: superuser
        privileges:
        - sys-admin
        - user-admin
        - data-admin
        - read-write-udf
        whitelist: []
    ```

- `aerospikeConfig`
    - This is a YAML representation of the `aerospike.conf` file.
      See [Aerospike Configuration](https://github.com/aerospike/aerospike-kubernetes-operator/wiki/Aerospike-configuration)
      for more details.

  Example,
    ```yaml
    aerospikeConfig:
      service:
        feature-key-file: /etc/aerospike/secrets/features.conf

      namespaces:
      - name: test
        memory-size: 3000000000
        replication-factor: 2
        storage-engine:
          type: memory
      - name: bar
        memory-size: 3000000000
        replication-factor: 2
        storage-engine:
          type: device
          files:
          - /opt/aerospike/data/bar.dat
          filesize: 2000000000
          data-in-memory: true
    ```

- `aerospikeNetworkPolicy`
  | Field | Type | Values | Description | | ----- | ---- | -------- | ----------- | | `access` | `string` | `pod`
  , `hostInternal`, `hostExternal` | type of network address to use for Aerospike access address | | `alternateAccess`
  | `string` | `pod`, `hostInternal`, `hostExternal` | type of network address to use for Aerospike alternate access
  address | | `tlsAccess` | `string` | `pod`, `hostInternal`, `hostExternal` | type of network address to use for
  Aerospike TLS access address | | `tlsAlternateAccess` | `string` | `pod`, `hostInternal`, `hostExternal` | type of
  network address to use for Aerospike TLS alternate access address |

  Example,
    ```yaml
    aerospikeNetworkPolicy:
      access: pod
      alternateAccess: hostInternal
      tlsAccess: pod
      tlsAlternateAccess: hostExternal
    ```

- `podSpec`

  | Field | Type | Sub-type | Description |
          | ----- | ---- | -------- | ----------- |
  | `sidecars` | `array` | `Container` (Format is same as defining containers in pod spec. Refer https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#container-v1-core) | Sidecar containers to add to the Aerospike pod |

  Example,
    ```yaml
    podSpec:
      sidecars:
      - name: aerospike-prometheus-exporter
        image: "aerospike/aerospike-prometheus-exporter:1.1.6"
        ports:
        - containerPort: 9145
          name: exporter
        env:
        - name: AGENT_LOG_LEVEL
          value: "debug"
    ```

- `rackConfig`

  | Field | Type | Sub-type | Description |
          | ----- | ---- | -------- | ----------- |
  | `namespaces` | `array` | `string` | List of namespaces to enable rack awareness |
  | `racks` | `array` | `Rack` | List of racks and their configurations |

    - Type `Rack`

      | Field | Type | Description |
                        | ----- | ---- | ----------- |
      | `aerospikeConfig` | Fields and their types are same as `AerospikeCluster.spec.aerospikeConfig` | Aerospike configuration |
      | `id` | `integer` | Identifier for the rack |
      | `nodeName` | `string` | Kubernetes node name for setting rack affinity. Rack pods will be deployed in given kubernetes node |
      | `rackLabel` | `string` | Rack label for setting rack affinity. Rack pods will be deployed in kubernetes nodes having rackLabel `{aerospike.com/rack-label:<rack-label>}` |
      | `region` | `string` | Region name for setting rack affinity. Rack pods will be deployed to given region |
      | `storage` | Fields and their types are same as `AerospikeCluster.spec.storage` | Storage configuration per rack |
      | `zone` | `string` | Zone name for setting rack affinity. Rack pods will be deployed to given Zone |

  Example,
    ```yaml
    rackConfig:
      namespaces:
        - test
      racks:
        - id: 1
          zone: us-west1-a
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
                - name: workdir
                  source:
                    persistentVolume:
                      storageClass: ssd
                      volumeMode: Filesystem
                      size: 1Gi
                  aerospike:
                    path: /opt/aerospike

                - name: ns
                  source:
                    persistentVolume:                  
                      storageClass: ssd
                      volumeMode: Filesystem
                      size: 3Gi
                  aerospike:
                    path: /opt/aerospike/data

        - id: 2
          zone: us-west1-b
          aerospikeConfig:
            service:
              proto-fd-max: 16000
    ```

- `storage`

  | Field | Type | Sub-type | Description |
          | ----- | ---- | -------- | ----------- |
  | `blockVolumePolicy` | `VolumePolicy` |  | BlockVolumePolicy contains default policies for block volumes |
  | `filesystemVolumePolicy` | `VolumePolicy` |  | FileSystemVolumePolicy contains default policies for filesystem volumes |
  | `volumes` | `array` | `Volume`  | List of volumes to be attached to pods |

    - Type `VolumePolicy`

      | Field | Type | Values | Description |
                  | ----- | ---- | -------- | ----------- |
      | `cascadeDelete` | `boolean` |  | CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster |
      | `initMethod` | `string` | `none`, `dd`, `blkdiscard`, `deleteFiles` | InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods comes up the first time. Defaults to "none" |

    - Type `Volume`

      | Field | Type | Values | Description |
                  | ----- | ---- | -------- | ----------- |
      | `cascadeDelete` | `boolean` |  | CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster |
      | `initMethod` | `string` | `none`, `dd`, `blkdiscard`, `deleteFiles` | InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods comes up the first time. Defaults to "none" |
      | `configMap` | `string` |  | Name of the configmap for 'configmap' mode volumes |
      | `path` | `string` |  | Device path or mount path for the volume |
      | `size` | `integer` |  | Size of volume |
      | `storageClass` | `string` |  | Storage class for volume provisioning |
      | `volumeMode` | `string` | `filesystem`, `block`, `configMap` | Volume mode |

  Example,
    ```yaml
    storage:
      filesystemVolumePolicy:
        cascadeDelete: false
        initMethod: deleteFiles
      volumes:
      - path: /opt/aerospike/data/test/
        storageClass: ssd
        volumeMode: Filesystem
        size: 3Gi
      - path: /opt/aerospike/data/bar/
        storageClass: ssd
        volumeMode: Filesystem
        size: 3Gi
    ```

- `validationPolicy`
  | Field | Type | Description | | ----- | ---- | ----------- | | `skipWorkDirValidate` | `boolean` |
  skipWorkDirValidate skips validation to check if Aerospike work directory is mounted on a persistent file storage.
  Defaults to false | | `skipXdrDlogFileValidate` | `boolean` | skipXdrDlogFileValidate skips validation to check if the
  xdr digestlog file is mounted on a persistent file storage. Defaults to false |

  Example,
    ```yaml
    validationPolicy:
      skipWorkDirValidate: true
    ```

- `resources`
  | Field | Description | | ----- | ----------- | | `requests` | Requests describes the minimum amount of resources
  required scheduling the pod | | `limits` | Limits describes the maximum amount of resources allowed for the pod |

  Example,
    ```yaml
    resources:
      requests:
        memory: 2Gi
        cpu: 200m
      limits:
        memory: 8Gi
        cpu: 800m
    ```
