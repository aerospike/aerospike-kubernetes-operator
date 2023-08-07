# Aerospike Kubernetes Operator Helm Chart

A Helm chart for Aerospike Kubernetes Operator

## Pre Requisites

- Kubernetes 1.16+

## Usage

<!-- ### Add Aerospike Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-operator
``` -->

### Clone this repository

```sh
git clone https://github.com/aerospike/aerospike-kubernetes-operator.git
cd aerospike-kubernetes-operator/helm-charts
```

### Deploy Cert-manager
Operator uses admission webhooks, which needs TLS certificates. These are issued by [cert-manager](https://cert-manager.io/docs/). Install cert-manager on your Kubernetes cluster using instructions [here](https://cert-manager.io/docs/installation/kubernetes/) before installing the operator.

### Deploy the Aerospike Kubernetes Operator

```sh
# helm install <chartName> <chartPath> --namespace <namespace>
helm install aerospike-kubernetes-operator ./aerospike-kubernetes-operator --set replicas=3
```

## Configurations

| Name                                | Description                                                                                           | Default                                                                                                           |
|-------------------------------------|-------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------|
| `replicas`                          | Number of operator replicas                                                                           | `2`                                                                                                               |
| `operatorImage.repository`          | Operator image repository                                                                             | `aerospike/aerospike-kubernetes-operator`                                                                         |
| `operatorImage.tag`                 | Operator image tag                                                                                    | `3.0.0`                                                                                                           |
| `operatorImage.pullPolicy`          | Image pull policy                                                                                     | `IfNotPresent`                                                                                                    |
| `imagePullSecrets`                  | Secrets containing credentials to pull Operator image from a private registry                         | `{}` (nil)                                                                                                        |
| `rbac.create`                       | Set this to `true` to let helm chart automatically create RBAC resources necessary for operator       | `true`                                                                                                            |
| `rbac.serviceAccountName`           | If `rbac.create=false`, provide a service account name to be used with the operator deployment        | `default`                                                                                                         |
| `healthPort`                        | Health port                                                                                           | `8081`                                                                                                            |
| `metricsPort`                       | Metrics port                                                                                          | `8080`                                                                                                            |
| `certs.create`                      | Set this to `true` to let helm chart automatically create certificates using `cert-manager`           | `true`                                                                                                            |
| `certs.webhookServerCertSecretName` | Kubernetes secret name which contains webhook server certificates                                     | `webhook-server-cert`                                                                                             |
| `watchNamespaces`                   | Namespaces to watch. Operator will watch for `AerospikeCluster` custom resources in these namespaces. | `default`                                                                                                         |
| `aerospikeKubernetesInitRegistry`   | Registry used to pull aerospike-init image                                                            | `docker.io`                                                                                                       |
| `resources`                         | Resource requests and limits for the operator pods                                                    | `{}` (nil)                                                                                                        |
| `affinity`                          | Affinity rules for the operator deployment                                                            | `{}` (nil)                                                                                                        |
| `extraEnv`                          | Extra environment variables that will be passed into the operator pods                                | `{}` (nil)                                                                                                        |
| `nodeSelector`                      | Node selectors for scheduling the operator pods based on node labels                                  | `{}` (nil)                                                                                                        |
| `tolerations`                       | Tolerations for scheduling the operator pods based on node taints                                     | `{}` (nil)                                                                                                        |
| `annotations`                       | Annotations for the operator deployment                                                               | `{}` (nil)                                                                                                        |
| `labels`                            | Labels for the operator deployment                                                                    | `{}` (nil)                                                                                                        |
| `podAnnotations`                    | Annotations for the operator pods                                                                     | `{}` (nil)                                                                                                        |
| `podLabels`                         | Labels for the operator pods                                                                          | `{}` (nil)                                                                                                        |
| `metricsService.labels`             | Labels for the operator's metrics service                                                             | `{}` (nil)                                                                                                        |
| `metricsService.annotations`        | Annotations for the operator's metrics service                                                        | `{}` (nil)                                                                                                        |
| `metricsService.port`               | Operator's metrics service port                                                                       | `8443`                                                                                                            |
| `metricsService.type`               | Operator's metrics service type                                                                       | `ClusterIP`                                                                                                       |
| `webhookService.labels`             | Labels for the operator's webhook service                                                             | `{}` (nil)                                                                                                        |
| `webhookService.annotations`        | Annotations for the operator's webhook service                                                        | `{}` (nil)                                                                                                        |
| `webhookService.port`               | Operator's webhook service port                                                                       | `443`                                                                                                             |
| `webhookService.targetPort`         | Operator's webhook target port                                                                        | `9443`                                                                                                            |
| `webhookService.type`               | Operator's webhook service type                                                                       | `ClusterIP`                                                                                                       |
| `podSecurityContext`                | Security context for the operator pods                                                                | `{}` (nil)                                                                                                        |
| `securityContext`                   | Security context for the operator container                                                           | `{}` (nil)                                                                                                        |
| `livenessProbe`                     | Liveliness probe for operator container                                                               | `initialDelaySeconds: 15`, `periodSeconds: 20`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3` |
| `readinessProbe`                    | Readiness probe for the operator container                                                            | `initialDelaySeconds: 5`, `periodSeconds: 10`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3`  |
| `kubeRBACProxy.image.repository`    | Kube RBAC Proxy image repository container                                                            | `gcr.io/kubebuilder/kube-rbac-proxy`                                                                              |
| `kubeRBACProxy.image.tag`           | Kube RBAC Proxy image tag                                                                             | `v0.13.1`                                                                                                         |
| `kubeRBACProxy.image.pullPolicy`    | Kube RBAC Proxy image pull policy                                                                     | `IfNotPresent`                                                                                                    |
| `kubeRBACProxy.port`                | Kube RBAC proxy listening port                                                                        | `8443`                                                                                                            |
| `kubeRBACProxy.resources`           | Kube RBAC Proxy container resource                                                                    | `{}` (nil)                                                                                                        |
<!-- ## Next Steps

Deploy [Aerospike Cluster](https://artifacthub.io/packages/helm/aerospike/aerospike-cluster) -->
