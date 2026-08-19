# Aerospike Cluster (Custom Resource) Helm Chart

A Helm chart for `AerospikeCluster` custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.23+
- Aerospike Kubernetes Operator

## Usage

### Add Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-enterprise
helm repo update
```

### Deploy Aerospike Cluster

#### Create a namespace

`<namespace>` used to install the aerospike chart must be included in `watchNamespaces` value of
aerospike-kubernetes-operator's `values.yaml`.

```sh
kubectl create namespace <namespace>
```

#### Create a ServiceAccount and RBAC for the Aerospike cluster pods

Create a `ServiceAccount` named `aerospike-operator-controller-manager` in the target namespace. This is the ServiceAccount used by the Aerospike cluster pods.

```sh
kubectl create serviceaccount aerospike-operator-controller-manager --namespace <namespace>
```

Bind this ServiceAccount to the `aerospike-cluster` ClusterRole (created by the aerospike-kubernetes-operator helm chart). Use a `RoleBinding` if the cluster only needs to be accessed from within the same Kubernetes cluster, or a `ClusterRoleBinding` if it needs to be accessible externally.

```sh
# RoleBinding - scoped to <namespace>
kubectl create rolebinding aerospike-cluster --namespace <namespace> \
    --clusterrole=aerospike-cluster --serviceaccount=<namespace>:aerospike-operator-controller-manager

# OR ClusterRoleBinding - cluster-wide
kubectl create clusterrolebinding aerospike-cluster \
    --clusterrole=aerospike-cluster --serviceaccount=<namespace>:aerospike-operator-controller-manager
```

_For multiple namespaces, add additional `--serviceaccount=<namespace>:aerospike-operator-controller-manager` subjects to the same `ClusterRoleBinding` (edit it with `kubectl edit clusterrolebinding aerospike-cluster`)._

#### Create a secret containing aerospike feature key file - `features.conf`

```sh
kubectl create secret generic aerospike-secret --from-file=<path-to-features.conf-file> --namespace <namespace>
```

#### Install the chart

```sh
# helm install <chartName> <chartPath> --namespace <namespace>
helm install aerospike aerospike/aerospike-cluster --namespace <namespace> --set devMode=true
```

_Note that this command assumes few defaults and deploys an aerospike cluster in **"dev"** mode with no data persistence. It is recommended to create a separate YAML file with configurations as per your requirements and use it
with `helm install`._

```sh
helm install aerospike aerospike/aerospike-cluster --namespace <namespace> \
    -f <customized-values-yaml-file>
```

## Configurations

| Name                        | Description                                                                                                                  | Default                                                   |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| `replicas`                  | Aerospike cluster size                                                                                                       | `3`                                                       |
| `image.repository`          | Aerospike server container image repository                                                                                  | `aerospike/aerospike-server-enterprise`                   |
| `image.tag`                 | Aerospike server container image tag                                                                                         | `8.1.2.0`                                                 |
| `imagePullSecrets`          | Secrets containing credentials to pull Aerospike container image from a private registry                                     | `{}` (nil)                                                |
| `customLabels`              | Custom labels to add on the aerospikecluster resource                                                                        | `{}` (nil)                                                |
| `aerospikeAccessControl`    | Aerospike access control configuration. Define users and roles to be created on the cluster.                                 | `{}` (nil)                                                |
| `aerospikeConfig`           | Aerospike configuration                                                                                                      | `{}` (nil)                                                |
| `aerospikeNetworkPolicy`    | Network policy (client access configuration)                                                                                 | `{}` (nil)                                                |
| `commonName`                | Base string for naming pods, services, stateful sets, etc.                                                                   | Release name truncated to 63 characters (without hyphens) |
| `podSpec`                   | Aerospike pod spec configuration                                                                                             | `{}` (nil)                                                |
| `rackConfig`                | Aerospike rack configuration                                                                                                 | `{}` (nil)                                                |
| `storage`                   | Aerospike pod storage configuration                                                                                          | `{}` (nil)                                                |
| `validationPolicy`          | Validation policy                                                                                                            | `{}` (nil)                                                |
| `operatorClientCert`        | Client certificates to connect to Aerospike                                                                                  | `{}` (nil)                                                |
| `seedsFinderServices`       | Service (e.g. loadbalancer) for Aerospike cluster discovery                                                                  | `{}` (nil)                                                |
| `maxUnavailable`            | maxUnavailable defines percentage/number of pods that can be allowed to go down or unavailable before application disruption | `1`                                                       |
| `disablePDB`                | Disable the PodDisruptionBudget creation for the Aerospike cluster                                                           | `false`                                                   |
| `enableDynamicConfigUpdate` | enableDynamicConfigUpdate enables dynamic config update flow of the operator                                                 | `false`                                                   |
| `enableRackIDOverride`      | enableRackIDOverride enables allocation of rack IDs to Aerospike pods after they are scheduled on Kubernetes nodes           | `false`                                                   |
| `rosterNodeBlockList`       | rosterNodeBlockList is a list of blocked nodeIDs from roster in a strong-consistency setup                                   | `[]`                                                      |
| `k8sNodeBlockList`          | k8sNodeBlockList is a list of Kubernetes nodes which are not used for Aerospike pods                                         | `[]`                                                      |
| `paused`                    | Pause reconciliation of the cluster                                                                                          | `false`                                                   |
| `devMode`                   | Deploy Aerospike cluster in dev mode                                                                                         | `false`                                                   |
| `operations`                | Operations is a list of on-demand operations to be performed on the Aerospike cluster.                                       | `[]`                                                      |

### Default values in "dev" mode (`devMode=true`):

The following values are set as defaults when the cluster is deployed in "dev" mode.

```yaml
aerospikeConfig:
  service:
    feature-key-file: /etc/aerospike/secrets/features.conf
    cgroup-mem-tracking: true

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
      replication-factor: 2
      storage-engine:
        type: memory
        data-size: 1073741824 # 1GiB

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
```

### Configurations Explained

Refer to [AerospikeCluster Customer Resource Spec](https://aerospike.com/docs/cloud/kubernetes/operator/configuration/Cluster-configuration-settings#spec) for details on above [configuration fields](#Configurations)
