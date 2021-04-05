# Aerospike Kubernetes Operator Helm Chart

A Helm chart for Aerospike Kubernetes Operator

## Pre Requisites

- Kubernetes 1.13+

## Usage

### Add Aerospike Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-operator
```

### Deploy the Aerospike Kubernetes Operator

```sh
helm install operator aerospike/aerospike-kubernetes-operator \
    --set replicas=3
```

## Configurations

| Name       | Description | Default   |
| ---------- | ----------- | --------- |
| `replicas` | Number of operator replicas | `2` |
| `image.repository` | Operator image repository | `aerospike/aerospike-kubernetes-operator` |
| `image.tag` | Operator image tag | `1.0.0` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `rbac.create` | Set this to `true` to let helm chart automatically create RBAC resources necessary for operator | `true` |
| `rbac.serviceAccountName` | If `rbac.create=false`, provide a service account name to be used with the operator deployment | `default` |
| `containerPort` | Operator container port | `8443` |
| `watchNamespaces` | Namespaces to watch. Operator will watch for `AerospikeCluster` custom resources in these namespaces | `default` |
| `logLevel` | Logging level for operator | `info` |
| `resources` | Resource requests and limits for the operator pods | `{}` (nil) |
| `affinity` | Affinity rules for the operator deployment | `{}` (nil) |
| `extraEnv` | Extra environment variables that will be passed into the operator pods | `{}` (nil) |
| `nodeSelector` | Node selectors for scheduling the operator pods based on node labels | `{}` (nil) |
| `tolerations` | Tolerations for scheduling the operator pods based on node taints | `{}` (nil) |
| `annotations` | Annotations for the operator deployment | `{}` (nil) |
| `labels` | Labels for the operator deployment | `{}` (nil) |
| `podAnnotations` | Annotations for the operator pods | `{}` (nil) |
| `podLabels` | Labels for the operator pods | `{}` (nil) |
| `service.labels` | Labels for the operator service | `{}` (nil) |
| `service.annotations` | Annotations for the operator service | `{}` (nil) |
| `service.port` | Operator service port | `443` |
| `service.type` | Operator service type | `ClusterIP` |
| `podSecurityContext` | Security context for the operator pods | `{}` (nil) |
| `securityContext` | Security context for the operator container | `{}` (nil) |
| `livenessProbe` | Liveness probe for operator container | `{}` (nil) |
| `readinessProbe` | Readiness probe for the operator container | `{}` (nil) |