# Aerospike Cluster (Custom Resource) Helm Chart

A Helm chart for `AerospikeCluster` custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.16+
- Aerospike Kubernetes Operator

## Usage

### Clone this repository

```sh
git clone https://github.com/aerospike/aerospike-kubernetes-operator.git
cd aerospike-kubernetes-operator/helm-charts
```

### Deploy Aerospike Cluster

#### Create a secret containing aerospike feature key file - `features.conf`

```sh
kubectl create secret generic aerospike-secret --from-file=<path-to-features.conf-file> --namespace <namespace>
```

#### Install the chart
`<namespace>` used to install aerospike chart must be included in `watchNamespaces` value of aerospike-kubernetes-operator's `values.yaml`

```sh
# helm install <chartName> <chartPath> --namespace <namespace>
helm install aerospike ./aerospike-cluster --set devMode=true
```


*Note that this command assumes few defaults and deploys an aerospike cluster in **"dev"** mode with no data
persistence. It is recommended to create a separate YAML file with configurations as per your requirements and use it
with `helm install`.*

```sh
helm install aerospike ./aerospike-cluster/ \
    -f <customized-values-yaml-file>
```

## Configurations

| Name       | Description | Default   |
| ---------- | ----------- | --------- |
| `replicas` | Aerospike cluster size | `3` |
| `image.repository` | Aerospike server container image repository | `aerospike/aerospike-server-enterprise` |
| `image.tag` | Aerospike server container image tag | `6.4.0.0` |
| `imagePullSecrets` | Secrets containing credentials to pull Aerospike container image from a private registry | `{}` (nil) |
| `customLabels` | Custom labels to add on the aerospikecluster resource | `{}` (nil) |
| `aerospikeAccessControl` | Aerospike access control configuration. Define users and roles to be created on the cluster. | `{}` (nil) |
| `aerospikeConfig` | Aerospike configuration | `{}` (nil) |
| `aerospikeNetworkPolicy` | Network policy (client access configuration) | `{}` (nil) |
| `commonName` | Base string for naming pods, services, stateful sets, etc.  | Release name truncated to 63 characters (without hyphens) |
| `podSpec` | Aerospike pod spec configuration | `{}` (nil) |
| `rackConfig` | Aerospike rack configuration | `{}` (nil) |
| `storage` | Aerospike pod storage configuration | `{}` (nil) |
| `validationPolicy` | Validation policy | `{}` (nil) |
| `operatorClientCert` | Client certificates to connect to Aerospike | `{}` (nil) |
| `seedsFinderServices` | Service (e.g. loadbalancer) for Aerospike cluster discovery | `{}` (nil) |
| `devMode` | Deploy Aerospike cluster in dev mode | `false` |

### Default values in "dev" mode (`devMode=true`):

The following values are set as defaults when the cluster is deployed in "dev" mode.

```yaml
aerospikeConfig:
  service:
    feature-key-file: /etc/aerospike/secrets/features.conf

  security:
    enable-security: false

  network:
    service:
      port: 3000
    fabric:
      port: 3001
    heartbeat:
      port: 3002

  namespaces:
    - name: test
      memory-size: 1073741824 # 1GiB
      replication-factor: 2
      storage-engine:
        type: memory

podSpec:
  multiPodPerHost: true

storage:
  volumes:
  - name: aerospike-config-secret
    source:
      secret:
        secretName: aerospike-secret
    aerospike:
      path: /etc/aerospike/secrets

validationPolicy:
  skipWorkDirValidate: true
  skipXdrDlogFileValidate: true
```

### Configurations Explained

Refer to [AerospikeCluster Customer Resource Spec](https://docs.aerospike.com/cloud/kubernetes/operator/cluster-configuration-settings#spec) for details on above [configuration fields](#Configurations)
