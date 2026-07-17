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

## Values validation

`values.yaml` is validated against `values.schema.json` by Helm on every `install`/`upgrade`/`template`, so bad values
fail fast with a pointer to the offending field instead of producing a broken manifest.

**`values.schema.json` is generated — do not edit it by hand.** It is built from `values.yaml` by the
[`helm-values-schema-json`](https://github.com/losisin/helm-values-schema-json) plugin. Validation rules live in
`# @schema ...` comments in `values.yaml`, field descriptions in `# -- ...` comments, and generator settings in
`.schema.yaml`. Anything written directly into the JSON is lost the next time the generator runs.

### Adding or changing a value

1. Add the key to `values.yaml` with a `# -- ` description, plus any `# @schema` constraints
   (see the [annotation reference](https://github.com/losisin/helm-values-schema-json/blob/main/docs/README.md)).
2. Regenerate and commit the schema:

   ```sh
   helm plugin install https://github.com/losisin/helm-values-schema-json   # once; add --verify=false on Helm v4
   cd helm-charts/aerospike-kubernetes-operator
   helm schema
   ```

   The `helm-schema-aerospike-kubernetes-operator` pre-commit hook does this automatically if you have
   [pre-commit](https://pre-commit.com/) installed. CI fails the PR when the committed schema is stale.
3. Verify with `helm schema lint --strict`, `helm lint .`, and `helm unittest .` (test suites live in `tests/`).

Two things to know when editing `values.yaml`:

- A key that is **commented out or absent is invisible to the generator**, and because the root schema sets
  `additionalProperties: false` it then gets *rejected* at install time. Keys that are optional in practice
  (`nameOverride`, `logging.level`, …) are therefore always present, defaulting to `""` (or, for
  `webhookServicePort`, annotated `# @schema type: [integer, null]` and defaulting to `null`).
- Kubernetes passthrough objects (`affinity`, `nodeSelector`, `podSecurityContext`, the probes, …) are intentionally
  left open so arbitrary upstream fields are accepted. Objects that should reject unknown keys opt in individually with
  `# @schema additionalProperties: false`.

## Configurations

| Name                                | Description                                                                                                                                                                                                               | Default                                                                                                            |
|-------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------|
| `replicas`                          | Number of operator replicas                                                                                                                                                                                               | `2`                                                                                                                |
| `operatorImage.repository`          | Operator image repository                                                                                                                                                                                                 | `aerospike/aerospike-kubernetes-operator`                                                                          |
| `operatorImage.tag`                 | Operator image tag                                                                                                                                                                                                        | `4.5.0`                                                                                                            |
| `operatorImage.pullPolicy`          | Image pull policy                                                                                                                                                                                                         | `IfNotPresent`                                                                                                     |
| `imagePullSecrets`                  | Secrets containing credentials to pull Operator image from a private registry                                                                                                                                             | `{}` (nil)                                                                                                         |
| `rbac.create`                       | Set this to `true` to let helm chart automatically create RBAC resources necessary for operator                                                                                                                           | `true`                                                                                                             |
| `rbac.serviceAccountName`           | If `rbac.create=false`, provide a service account name to be used with the operator deployment                                                                                                                            | `default`                                                                                                          |
| `healthPort`                        | Health port                                                                                                                                                                                                               | `8081`                                                                                                             |
| `metricsPort`                       | Metrics port                                                                                                                                                                                                              | `8443`                                                                                                             |
| `certs.webhook.create`              | When `true`, chart creates webhook TLS certificate resources via `cert-manager`                                                                                                                                          | `true`                                                                                                             |
| `certs.webhook.webhookServerCertSecretName` | Kubernetes `Secret` name for webhook serving certificates                                                                                                                                                           | `webhook-server-cert`                                                                                              |
| `certs.metrics.create`              | When `true`, chart creates metrics TLS certificate resources via `cert-manager` and mounts them on the manager                                                                                                          | `false`                                                                                                            |
| `certs.metrics.metricsServerCertSecretName` | Kubernetes `Secret` name for metrics serving certificates                                                                                                                                                         | `metrics-server-cert`                                                                                            |
| `watchNamespaces`                   | Namespaces to watch. Operator will watch for Aerospike custom resources in these namespaces (comma-separated).                                                                                                    | `default,aerospike`                                                                                               |
| `ignoreNamespaces`                  | Namespaces ignored by the eviction webhook (comma-separated)                                                                                                                                                              | `kube-system,kube-node-lease`                                                                                      |
| `safePodEviction.enable`            | Enable the eviction webhook to safely block Aerospike pod evictions during node maintenance. Also enables Prometheus metrics (`aerospike_ako_eviction_webhook_requests_total` with labels: eviction_namespace, decision). | `false`                                                                                                            |
| `safePodEviction.timeoutSeconds`    | Eviction webhook timeout in seconds when safePodEviction is enabled                                                                                                                                                       | `20`                                                                                                               |
| `failedPodGracePeriodSeconds`       | Grace period to delete/recover failed pods (in seconds)                                                                                                                                                                   | `60`                                                                                                               |
| `logging.development`               | Zap development profile (`--zap-devel`); keep `true` unless you want production-style logging                                                                                                                           | `true`                                                                                                             |
| `logging.level`                     | Optional; when set (non-empty), chart adds `--zap-log-level`. If omitted or `""`, no flag is emitted and Zap uses its default level (typically `info` unless overridden).                                              | `""` / omitted in chart defaults                                                                                   |
| `logging.encoder`                   | Optional; when set (non-empty), chart adds `--zap-encoder` (`json` or `console`). If omitted or `""`, no flag is emitted and Zap uses its default encoder.                                                               | `""` / omitted in chart defaults                                                                                   |
| `metrics.secure`                    | Passed as `--metrics-secure` on the manager; when `true`, serves `/metrics` over HTTPS with auth (controller-runtime default)                                                                                            | `true`                                                                                                             |
| `aerospikeKubernetesInitRegistry`   | Registry used to pull aerospike-init image                                                                                                                                                                                | `docker.io`                                                                                                        |
| `resources`                         | Resource requests and limits for the operator pods                                                                                                                                                                        | `limits`: cpu `400m`, memory `512Mi`; `requests`: cpu `10m`, memory `64Mi` (see `values.yaml`)                       |
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
| `securityContext`                   | Security context for the operator container                                                                                                                                                                               | `allowPrivilegeEscalation: false` (see `values.yaml`)                                                              |
| `livenessProbe`                     | Liveliness probe for operator container                                                                                                                                                                                   | `initialDelaySeconds: 15`, `periodSeconds: 20`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3`  |
| `readinessProbe`                    | Readiness probe for the operator container                                                                                                                                                                                | `initialDelaySeconds: 5`, `periodSeconds: 10`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3`   |
<!-- ## Next Steps

Deploy [Aerospike Cluster](https://artifacthub.io/packages/helm/aerospike/aerospike-cluster) -->
