# Aerospike Cluster (Custom Resource) Helm Chart

A Helm chart for `AerospikeCluster` custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.13+
- Aerospike Kubernetes Operator

## Usage

### Add Aerospike Helm Repository

<!-- TODO: Change/update the URL on publishing the chart -->

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-operator
```

### Deploy Aerospike Cluster

```sh
helm install aerospike aerospike/aerospike-cluster \
    --set-file featureKeyFile=<path-to-feature-key-file>
```

*Note that this command assumes few defaults and deploys an aerospike cluster with no data persistence. It is recommended to create a separate YAML file with configurations as per your requirements and use it with `helm install`.*

```sh
helm install aerospike aerospike/aerospike-cluster \
    --set-file featureKeyFile=<path-to-feature-key-file> \
    -f <customized-values-yaml-file>`
```

## Configurations

| Name       | Description | Default   |
| ---------- | ----------- | --------- |
| `replicas` | Aerospike cluster size | `3` |
| `image.repository` | Aerospike server container image repository | `aerospike/aerospike-server-enterprise` |
| `image.tag` | Aerospike server container image tag | `5.2.0.7` |
| `multiPodPerHost` | Set this to `true` to allow scheduling multiple pods per kubernetes node | `true` |
| `aerospikeAccessControl` | Aerospike access control configuration. Define users and roles to be created on the cluster. | `{}` (nil) |
| `aerospikeConfig` | Aerospike configuration | `{}` (nil) |
| `secrets` | Secrets to be mounted into containers at `/etc/aerospike/secrets/` | `{}` (nil) |
| `featureKeyFile` | Dynamic secret to pass in feature key file to run Aerospike enterprise edition (use only with `helm install` / `helm upgrade` command. Not to be specified in `values.yaml` file) | `nil` |
| `aerospikeNetworkPolicy` | Network policy (client access configuration) | `{}` (nil) |
| `podSpec` | Aerospike pod spec configuration | `{}` (nil) |
| `rackConfig` | Aerospike rack configuration | `{}` (nil) |
| `storage` | Aerospike pod storage configuration | `{}` (nil) |
| `validationPolicy` | Validation policy | `{}` (nil) |
| `resources` | Resource requests and limits for Aerospike pod | `requests.memory=1Gi, requests.cpu=100m` |

### Configurations Explained

- `aerospikeAccessControl`
    - Fields
        - `users`
            - type: `array`
            - sub-type: `User`
                - Fields
                    - `name` - Name of the user
                        - type: `string`
                    - `roles` - Roles for the user
                        - type: `array`
                        - sub-type: `string`
                    - `secretName` - Secret containing the password
                        - type: `string`
        - `adminPolicy`
            - Fields
                - `timeout` - Timeout for adminPolicy in client (in milliseconds)
                    - type: `integer`
        - `roles`
            - type: `array`
            - sub-type: `Role`
                - Fields
                    - `name` - Name of the role
                        - type: `string`
                    - `privileges` - Privileges for the role
                        - type: `array`
                        - sub-type: `string`
                    - `whitelist` - Whitelist of host address allowed for the role
                        - type: `array`
                        - sub-type: `string`

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
    - This is a YAML representation of the `aerospike.conf` file. So the configuration names and their types are same as in `aerospike.conf` file.

    Example,
    ```yaml
    aerospikeConfig:
      service:
        feature-key-file: /etc/aerospike/secrets/features.conf

      namespaces:
      - name: test
        memory-size: 3000000000
        replication-factor: 2
        storage-engine: memory
      - name: bar
        memory-size: 3000000000
        replication-factor: 2
        storage-engine:
          files:
          - /opt/aerospike/data/bar.dat
          filesize: 2000000000
          data-in-memory: true
    ```

- `secrets`
    - Fields
        - Format,

          `<secretName>:<base64EncodedValue>`

    Example,
    ```yaml
    secrets:
      server1.aerospike.com.cert.pem: <SomeBase64Value>
      server1.aerospike.com.encrypted.key.pem: <SomeBase64Value>
      server1.aerospike.com.key.pem: <SomeBase64Value>
      server1.aerospike.com.cacert.pem: <SomeBase64Value>
    ```

- `aerospikeNetworkPolicy`
    - Fields
        - `access` - type of network address to use for Aerospike access address
            - type: `string`
            - values: `pod`, `hostInternal`, `hostExternal`
        - `alternateAccess` - type of network address to use for Aerospike alternate access address
            - type: `string`
            - values: `pod`, `hostInternal`, `hostExternal`
        - `tlsAccess` - type of network address to use for Aerospike TLS access address
            - type: `string`
            - values: `pod`, `hostInternal`, `hostExternal`
        - `tlsAlternateAccess` - type of network address to use for Aerospike TLS alternate access address
            - type: `string`
            - values: `pod`, `hostInternal`, `hostExternal`

    Example,
    ```yaml
    aerospikeNetworkPolicy:
      access: pod
      alternateAccess: hostInternal
      tlsAccess: pod
      tlsAlternateAccess: hostExternal
    ```

- `podSpec`
    - Fields
        - `sidecars` - Sidecar containers to add to the Aerospike pod
            - type: `array`
            - sub-type: `Container`
                - Format is same as defining containers in pod spec
                - Refer https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.20/#container-v1-core

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
    - Fields
        - `namespaces` - List of namespaces to enable rack awareness
            - type: `array`
            - sub-type: `string`
        - `racks` - List of racks and their configurations
            - type: `array`
            - sub-type: `Rack`
                - Fields
                    - `aerospikeConfig`
                        - Fields are their types are same as `AerospikeCluster.spec.aerospikeConfig`
                    - `id` - Identifier for the rack
                        - type: `integer`
                    - `nodeName` - K8s Node name for setting rack affinity. Rack pods will be deployed in given k8s Node
                        - type: `string`
                    - `rackLabel` - Racklabel for setting rack affinity. Rack pods will be deployed in k8s nodes having rackLabel `{aerospike.com/rack-label:<rack-label>}`
                        - type: `string`
                    - `region` - Region name for setting rack affinity. Rack pods will be deployed to given Region
                        - type: `string`
                    - `storage`
                        - Fields and their types are same as `AerospikeCluster.spec.storage`
                    - `zone` - Zone name for setting rack affinity. Rack pods will be deployed to given Zone
                        - type: `string`

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
                - storageClass: ssd
                  path: /opt/aerospike
                  volumeMode: filesystem
                  sizeInGB: 1
                - path: /opt/aerospike/data
                  storageClass: ssd
                  volumeMode: filesystem
                  sizeInGB: 3
        - id: 2
          zone: us-west1-b
          aerospikeConfig:
            service:
              proto-fd-max: 16000
    ```

- `storage`
    - Fields
        - `blockVolumePolicy` - BlockVolumePolicy contains default policies for block volumes
            - Fields
                - `cascadeDelete` - CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster
                    - type: `boolean`
                - `initMethod` - InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods comes up the first time. Defaults to "none"
                    - type: `string`
                    - values: `none`, `dd`, `blkdiscard`, `deleteFiles`
        - `filesystemVolumePolicy` - FileSystemVolumePolicy contains default policies for filesystem volumes
            - Fields
                - `cascadeDelete` - CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster
                    - type: `boolean`
                - `initMethod` - InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods comes up the first time. Defaults to "none"
                    - type: `string`
                    - values: `none`, `dd`, `blkdiscard`, `deleteFiles`
        - `volumes` - List of volumes to be attached to pods
            - Fields
                - `cascadeDelete` - CascadeDelete determines if the persistent volumes are deleted after the pod this volume binds to is terminated and removed from the cluster
                    - type: `boolean`
                - `configMap` - Name of the configmap for 'configmap' mode volumes
                    - type: `string`
                - `initMethod` - InitMethod determines how volumes attached to Aerospike server pods are initialized when the pods comes up the first time. Defaults to "none"
                    - type: `string`
                    - values: `none`, `dd`, `blkdiscard`, `deleteFiles`
                - `path` - Device path or mount path for the volume
                    - type: `string`
                - `sizeInGB` - Size of volume in GB
                    - type: `integer`
                - `storageClass` - Storage class for volume provisioning
                    - type: `string`
                - `volumeMode`
                    - type: `string`
                    - values: `filesystem`, `block`, `configMap`

    Example,
    ```yaml
    storage:
      filesystemVolumePolicy:
        cascadeDelete: false
        initMethod: deleteFiles
      volumes:
      - path: /opt/aerospike/data/test/
        storageClass: ssd
        volumeMode: filesystem
        sizeInGB: 3
      - path: /opt/aerospike/data/bar/
        storageClass: ssd
        volumeMode: filesystem
        sizeInGB: 3
    ```

- `validationPolicy`
    - Fields
        - `skipWorkDirValidate` - skipWorkDirValidate skips validation to check if Aerospike work directory is mounted on a persistent file storage. Defaults to false
            - type: `boolean`
        - `skipXdrDlogFileValidate` - skipXdrDlogFileValidate skips validation to check if the xdr digestlog file is mounted on a persistent file storage. Defaults to false
            - type: `boolean`

    Example,
    ```yaml
    validationPolicy:
      skipWorkDirValidate: true
    ```

- `resources`
    - Fields
        - `requests` - Requests describes the minimum amount of compute resources required.
        - `limits` - Limits describes the maximum amount of compute resources allowed.

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