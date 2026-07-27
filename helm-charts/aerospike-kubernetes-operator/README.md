# Aerospike Kubernetes Operator Helm Chart

A Helm chart for Aerospike Kubernetes Operator

## Pre Requisites

- Kubernetes 1.23+

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

## Webhook TLS Configuration

By default, webhook TLS is managed by cert-manager. To manage webhook TLS externally, set `certs.webhook.caBundle` and pre-create the serving cert secret. See [Base64-encoded caBundle](#base64-encoded-cabundle) below.

For cert-manager-free installs, set `certs.webhook.create` to `false` and pre-create the serving certificate secret (`certs.webhook.webhookServerCertSecretName`, default `webhook-server-cert`) with `tls.crt` and `tls.key`. The operator deployment already mounts this secret — no deployment changes needed.

```sh
kubectl create secret tls webhook-server-cert \
  --cert=webhook-serving.crt \
  --key=webhook-serving.key \
  -n <namespace>

helm install aerospike-kubernetes-operator ./aerospike-kubernetes-operator \
  --namespace <namespace> \
  --set certs.webhook.create=false \
  --set certs.webhook.caBundle="${CA_BUNDLE}"
```

### Base64-encoded caBundle

`certs.webhook.caBundle` expects the **base64 encoding of the CA certificate PEM** that signed the webhook serving certificate. It is not the serving certificate itself.

When set, the chart injects `clientConfig.caBundle` on all webhooks and skips the `cert-manager.io/inject-ca-from` annotation.

**Encode the CA certificate:**

```sh
# Linux
CA_BUNDLE=$(base64 -w0 ca.crt)

# macOS
CA_BUNDLE=$(base64 -i ca.crt)
```

The result must be a single-line string with no line breaks.

**Pass via Helm:**

```sh
helm install aerospike-kubernetes-operator ./aerospike-kubernetes-operator \
  --set certs.webhook.caBundle="${CA_BUNDLE}"
```

Or in a values file:

```yaml
certs:
  webhook:
    caBundle: "LS0tLS1CRUdJTi..."   # base64-encoded CA PEM
```

## Configurations

| Name                                | Description                                                                                                                                                                                                               | Default                                                                                                            |
|-------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------|
| `replicas`                          | Number of operator replicas                                                                                                                                                                                               | `2`                                                                                                                |
| `operatorImage.repository`          | Operator image repository                                                                                                                                                                                                 | `aerospike/aerospike-kubernetes-operator`                                                                          |
| `operatorImage.tag`                 | Operator image tag                                                                                                                                                                                                        | `4.4.1`                                                                                                            |
| `operatorImage.pullPolicy`          | Image pull policy                                                                                                                                                                                                         | `IfNotPresent`                                                                                                     |
| `imagePullSecrets`                  | Secrets containing credentials to pull Operator image from a private registry                                                                                                                                             | `{}` (nil)                                                                                                         |
| `rbac.create`                       | Set this to `true` to let helm chart automatically create RBAC resources necessary for operator                                                                                                                           | `true`                                                                                                             |
| `rbac.serviceAccountName`           | If `rbac.create=false`, provide a service account name to be used with the operator deployment                                                                                                                            | `default`                                                                                                          |
| `healthPort`                        | Health port                                                                                                                                                                                                               | `8081`                                                                                                             |
| `metricsPort`                       | Metrics port                                                                                                                                                                                                              | `8080`                                                                                                             |
| `certs.webhook.create`              | Create webhook serving certificate via cert-manager                                                                                                                                                                       | `true`                                                                                                             |
| `certs.webhook.caBundle`            | Base64-encoded CA certificate for webhook `clientConfig.caBundle`. When set, cert-manager CA injection is skipped                                                                                                         | `""`                                                                                                               |
| `certs.webhook.webhookServerCertSecretName` | Kubernetes secret name which contains webhook server certificates (`tls.crt`, `tls.key`)                                                                                                                            | `webhook-server-cert`                                                                                              |
| `watchNamespaces`                   | Namespaces to watch. Operator will watch for `AerospikeCluster` custom resources in these namespaces.                                                                                                                     | `default`                                                                                                          |
| `safePodEviction.enable`            | Enable the eviction webhook to safely block Aerospike pod evictions during node maintenance. Also enables Prometheus metrics (`aerospike_ako_eviction_webhook_requests_total` with labels: eviction_namespace, decision). | `false`                                                                                                            |
| `safePodEviction.timeoutSeconds`    | Eviction webhook timeout in seconds when safePodEviction is enabled                                                                                                                                                       | `20`                                                                                                               |
| `failedPodGracePeriodSeconds`       | Grace period to delete/recover failed pods (in seconds)                                                                                                                                                                   | `60`                                                                                                               |
| `aerospikeKubernetesInitRegistry`   | Registry used to pull aerospike-init image                                                                                                                                                                                | `docker.io`                                                                                                        |
| `resources`                         | Resource requests and limits for the operator pods                                                                                                                                                                        | `{}` (nil)                                                                                                         |
| `affinity`                          | Affinity rules for the operator deployment                                                                                                                                                                                | `{}` (nil)                                                                                                         |
| `extraEnv`                          | Extra environment variables that will be passed into the operator pods                                                                                                                                                    | `{}` (nil)                                                                                                         |
| `nodeSelector`                      | Node selectors for scheduling the operator pods based on node labels                                                                                                                                                      | `{}` (nil)                                                                                                         |
| `tolerations`                       | Tolerations for scheduling the operator pods based on node taints                                                                                                                                                         | `{}` (nil)                                                                                                         |
| `annotations`                       | Annotations for the operator deployment                                                                                                                                                                                   | `{}` (nil)                                                                                                         |
| `labels`                            | Labels for the operator deployment                                                                                                                                                                                        | `{}` (nil)                                                                                                         |
| `podAnnotations`                    | Annotations for the operator pods                                                                                                                                                                                         | `{}` (nil)                                                                                                         |
| `podLabels`                         | Labels for the operator pods                                                                                                                                                                                              | `{}` (nil)                                                                                                         |
| `metricsService.labels`             | Labels for the operator's metrics service                                                                                                                                                                                 | `{}` (nil)                                                                                                         |
| `metricsService.annotations`        | Annotations for the operator's metrics service                                                                                                                                                                            | `{}` (nil)                                                                                                         |
| `metricsService.port`               | Operator's metrics service port                                                                                                                                                                                           | `8443`                                                                                                             |
| `metricsService.type`               | Operator's metrics service type                                                                                                                                                                                           | `ClusterIP`                                                                                                        |
| `webhookService.labels`             | Labels for the operator's webhook service                                                                                                                                                                                 | `{}` (nil)                                                                                                         |
| `webhookService.annotations`        | Annotations for the operator's webhook service                                                                                                                                                                            | `{}` (nil)                                                                                                         |
| `webhookService.port`               | Operator's webhook service port                                                                                                                                                                                           | `443`                                                                                                              |
| `webhookService.targetPort`         | Operator's webhook target port                                                                                                                                                                                            | `9443`                                                                                                             |
| `webhookService.type`               | Operator's webhook service type                                                                                                                                                                                           | `ClusterIP`                                                                                                        |
| `podSecurityContext`                | Security context for the operator pods                                                                                                                                                                                    | `{}` (nil)                                                                                                         |
| `securityContext`                   | Security context for the operator container                                                                                                                                                                               | `{}` (nil)                                                                                                         |
| `livenessProbe`                     | Liveliness probe for operator container                                                                                                                                                                                   | `initialDelaySeconds: 15`, `periodSeconds: 20`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3`  |
| `readinessProbe`                    | Readiness probe for the operator container                                                                                                                                                                                | `initialDelaySeconds: 5`, `periodSeconds: 10`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3`   |
<!-- ## Next Steps

Deploy [Aerospike Cluster](https://artifacthub.io/packages/helm/aerospike/aerospike-cluster) -->
