# Aerospike Kubernetes Operator Helm Chart

A Helm chart for Aerospike Kubernetes Operator

## Pre Requisites

- Kubernetes 1.23+

## Usage

### Add Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-enterprise
helm repo update
```

### Create Namespace

Create the namespace where AKO will be installed. Replace the placeholder `<namespace>` with your own namespace name. If `--namespace` is not given during install, AKO uses a namespace called `default`.

```sh
kubectl create namespace <namespace>
```

### Deploy Cert-manager (conditional)

The operator uses admission webhooks, which need TLS certificates. How those certificates
are provided is selected by `certs.webhook.provider`:

| `certs.webhook.provider` | Who provides the certificate | cert-manager required |
| --- | --- | --- |
| `certManager` (default) | Chart creates a cert-manager `Issuer` and `Certificate`; cainjector fills in the `caBundle` | Yes |
| `selfSigned` | Chart generates a CA and serving certificate and creates the `Secret` itself | No |
| `external` | You pre-create the `Secret` and supply `certs.webhook.external.caBundle` | No |

cert-manager is also required whenever `certs.metrics.create` is `true`, regardless of the
webhook provider — the metrics certificate is cert-manager-only.

If you set `certs.webhook.create: false` on 4.5.0 or earlier, leave it as it is: that
combination still renders exactly what it did before — the chart creates no `Issuer` and
no `Certificate`, but keeps the `cert-manager.io/inject-ca-from` annotation, so cainjector
fills in the `caBundle` from the `Certificate` named `aerospike-operator-serving-cert` that
you manage yourself. `certs.webhook.create` is deprecated but still honoured. Do not switch
such a release to `provider: certManager` unless you also delete your own `Certificate`,
because the chart would then create one with the same name and `secretName`.

If you need cert-manager, install it using the instructions
[here](https://cert-manager.io/docs/installation/kubernetes/) before installing the operator.

#### provider: selfSigned (development and POC only)

```sh
helm install aerospike-kubernetes-operator aerospike/aerospike-kubernetes-operator \
  --namespace <namespace> --set certs.webhook.provider=selfSigned
```

The chart generates the certificate and reuses the existing `Secret` on subsequent
upgrades so that `helm upgrade` does not rotate it. Two caveats make this unsuitable for
production:

* The certificate is valid for 10 years and nothing renews it.
* Its validity window starts at the clock of the machine that runs `helm`. If that clock
  runs ahead of the cluster's, the API server sees the certificate as not yet valid and
  rejects admission until the skew elapses.
* Helm's `lookup` returns nothing under `helm template` and `--dry-run`, so a pipeline
  that renders manifests and applies them regenerates the certificate on every apply.
  Because the API server sees the new `caBundle` immediately while the operator pod picks
  up the remounted `Secret` on kubelet's schedule, and the webhooks use
  `failurePolicy: Fail`, that gap is a real admission outage. Use `certManager` or
  `external` for GitOps.

#### provider: external

Pre-create the serving certificate `Secret` (default name `webhook-server-cert`) with
`tls.crt` and `tls.key`, then pass the base64-encoded PEM of the CA that signed it:

```sh
# `< ca.crt` rather than `base64 ca.crt`, which BSD/macOS base64 rejects
base64 < ca.crt > ca.b64

helm install aerospike-kubernetes-operator aerospike/aerospike-kubernetes-operator \
  --namespace <namespace> \
  --set certs.webhook.provider=external \
  --set-file certs.webhook.external.caBundle=ca.b64
```

Line wrapping and a trailing newline are both fine — the chart strips whitespace before
rendering, because the API server itself decodes `caBundle` with strict base64.

Note this is the CA certificate that signed the serving certificate, not the serving
certificate itself; passing raw PEM is rejected at install time. The manager `Deployment`
mounts the `Secret` unconditionally, so its pods stay in `ContainerCreating` until you
create it.

#### Switching an existing release between providers

**To `selfSigned`.** This makes the chart the owner of the serving-certificate `Secret`,
which it does not own under the other providers — under `certManager` that `Secret` belongs
to cert-manager. Helm will not adopt a resource it does not own, so use one of the two
routes below. With neither, the upgrade stops with Helm's own error naming the `Secret` and
the ownership markers it is missing.

*Preferred — let Helm adopt the existing `Secret`.* The chart reuses the certificate
already in the `Secret`, so the served certificate does not change and there is no
admission gap:

```sh
helm upgrade aerospike-kubernetes-operator ... --take-ownership \
  --set certs.webhook.provider=selfSigned
```

*Or delete and regenerate.* Delete the cert-manager `Certificate` **first** — while it
exists, cert-manager re-issues the `Secret` within seconds and the upgrade fails again:

```sh
kubectl delete certificate aerospike-operator-serving-cert -n <namespace> --ignore-not-found
kubectl delete secret webhook-server-cert -n <namespace>
helm upgrade ... --set certs.webhook.provider=selfSigned
```

This regenerates the certificate, so expect a brief admission outage: the API server sees
the new `caBundle` immediately while the operator pod picks up the remounted `Secret` on
kubelet's schedule.

**Away from `selfSigned`** (to `certManager` or `external`) also has an outage window.
Helm removes the inlined `caBundle` from every webhook entry and deletes the chart-owned
`Secret`, and admission fails until cert-manager issues a new certificate, cainjector
writes the CA into both webhook configurations, and the pod remounts the `Secret`. Confirm
cert-manager is actually installed before switching to `certManager`, or the `Secret` is
simply gone and the manager pods will not start.

### Deploy the Aerospike Kubernetes Operator

Install AKO on your Kubernetes cluster, pinning the chart `--version` to the release you want to install.

```sh
# helm install <chartName> <chartPath> --namespace <namespace> --version <chartVersion>
helm install aerospike-kubernetes-operator aerospike/aerospike-kubernetes-operator --namespace <namespace> --version 4.5.0 --set watchNamespaces="aerospike"
```

## Configurations

| Name                                        | Description                                                                                                                                                                                                               | Default                                                                                                           |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `replicas`                                  | Number of operator replicas                                                                                                                                                                                               | `2`                                                                                                               |
| `nameOverride`                              | Override the chart name used in generated resource names                                                                                                                                                                  | `""`                                                                                                              |
| `fullnameOverride`                          | Override the full generated resource name                                                                                                                                                                                 | `""`                                                                                                              |
| `operatorImage.repository`                  | Operator image repository                                                                                                                                                                                                 | `aerospike/aerospike-kubernetes-operator`                                                                         |
| `operatorImage.tag`                         | Operator image tag                                                                                                                                                                                                        | `4.5.0`                                                                                                           |
| `operatorImage.pullPolicy`                  | Image pull policy                                                                                                                                                                                                         | `IfNotPresent`                                                                                                    |
| `imagePullSecrets`                          | Secrets containing credentials to pull Operator image from a private registry                                                                                                                                             | `[]` (nil)                                                                                                        |
| `rbac.create`                               | Set this to `true` to let helm chart automatically create RBAC resources necessary for operator                                                                                                                           | `true`                                                                                                            |
| `rbac.serviceAccountName`                   | If `rbac.create=false`, provide a service account name to be used with the operator deployment                                                                                                                            | `default`                                                                                                         |
| `healthPort`                                | Health port                                                                                                                                                                                                               | `8081`                                                                                                            |
| `metricsPort`                               | Metrics port                                                                                                                                                                                                              | `8443`                                                                                                            |
| `certs.webhook.provider`                    | How the webhook serving certificate is provided: `certManager`, `selfSigned` or `external`. Empty infers the mode from the deprecated `certs.webhook.create`                                                               | `""`                                                                                                              |
| `certs.webhook.create`                      | **DEPRECATED**, use `certs.webhook.provider`. Only consulted when `provider` is empty; `false` keeps 4.5.0 behaviour                                                                                                                                     | `true`                                                                                                            |
| `certs.webhook.external.caBundle`           | Base64-encoded PEM CA bundle that signed the webhook serving certificate. Required when `provider` is `external`; wrapping/trailing whitespace stripped                                                                              | `""`                                                                                                              |
| `certs.webhook.webhookServerCertSecretName` | Kubernetes `Secret` name for webhook serving certificates                                                                                                                                                                 | `webhook-server-cert`                                                                                             |
| `certs.metrics.create`                      | When `true`, chart creates metrics TLS certificate resources via `cert-manager` and mounts them on the manager                                                                                                            | `false`                                                                                                           |
| `certs.metrics.metricsServerCertSecretName` | Kubernetes `Secret` name for metrics serving certificates                                                                                                                                                                 | `metrics-server-cert`                                                                                             |
| `watchNamespaces`                           | Namespaces to watch. Operator will watch for Aerospike custom resources in these namespaces (comma-separated).                                                                                                            | `default,aerospike`                                                                                               |
| `ignoreNamespaces`                          | Namespaces ignored by the eviction webhook (comma-separated)                                                                                                                                                              | `kube-system,kube-node-lease`                                                                                     |
| `safePodEviction.enable`                    | Enable the eviction webhook to safely block Aerospike pod evictions during node maintenance. Also enables Prometheus metrics (`aerospike_ako_eviction_webhook_requests_total` with labels: eviction_namespace, decision). | `false`                                                                                                           |
| `safePodEviction.timeoutSeconds`            | Eviction webhook timeout in seconds when safePodEviction is enabled                                                                                                                                                       | `20`                                                                                                              |
| `failedPodGracePeriodSeconds`               | Grace period to delete/recover failed pods (in seconds)                                                                                                                                                                   | `60`                                                                                                              |
| `logging.development`                       | Zap development profile (`--zap-devel`); keep `true` unless you want production-style logging                                                                                                                             | `true`                                                                                                            |
| `logging.level`                             | Optional; when set (non-empty), chart adds `--zap-log-level`. If omitted or `""`, no flag is emitted and Zap uses its default level (typically `info` unless overridden).                                                 | `""` / omitted in chart defaults                                                                                  |
| `logging.encoder`                           | Optional; when set (non-empty), chart adds `--zap-encoder` (`json` or `console`). If omitted or `""`, no flag is emitted and Zap uses its default encoder.                                                                | `""` / omitted in chart defaults                                                                                  |
| `metrics.secure`                            | Passed as `--metrics-secure` on the manager; when `true`, serves `/metrics` over HTTPS with auth (controller-runtime default)                                                                                             | `true`                                                                                                            |
| `aerospikeKubernetesInitRegistry`           | Registry used to pull aerospike-init image                                                                                                                                                                                | `docker.io`                                                                                                       |
| `aerospikeKubernetesInitRegistryNamespace`  | Namespace in registry used to pull aerospike-init image                                                                                                                                                                   | `aerospike`                                                                                                       |
| `aerospikeKubernetesInitNameTag`            | Name and tag of aerospike-init image, as `name:tag`                                                                                                                                                                       | `aerospike-kubernetes-init:2.5.3`                                                                                 |
| `resources`                                 | Resource requests and limits for the operator pods                                                                                                                                                                        | `limits`: cpu `400m`, memory `512Mi`; `requests`: cpu `10m`, memory `64Mi` (see `values.yaml`)                    |
| `affinity`                                  | Affinity rules for the operator deployment                                                                                                                                                                                | `{}` (nil)                                                                                                        |
| `extraEnv`                                  | Extra environment variables that will be passed into the operator pods                                                                                                                                                    | `{}` (nil)                                                                                                        |
| `nodeSelector`                              | Node selectors for scheduling the operator pods based on node labels                                                                                                                                                      | `{}` (nil)                                                                                                        |
| `tolerations`                               | Tolerations for scheduling the operator pods based on node taints                                                                                                                                                         | `[]` (nil)                                                                                                        |
| `annotations`                               | Annotations for the operator deployment                                                                                                                                                                                   | `{}` (nil)                                                                                                        |
| `labels`                                    | Labels for the operator deployment                                                                                                                                                                                        | `{}` (nil)                                                                                                        |
| `podAnnotations`                            | Annotations for the operator pods                                                                                                                                                                                         | `{}` (nil)                                                                                                        |
| `podLabels`                                 | Labels for the operator pods                                                                                                                                                                                              | `{}` (nil)                                                                                                        |
| `metricsService.labels`                     | Labels for the operator's metrics service                                                                                                                                                                                 | `{}` (nil)                                                                                                        |
| `metricsService.annotations`                | Annotations for the operator's metrics service                                                                                                                                                                            | `{}` (nil)                                                                                                        |
| `metricsService.port`                       | Operator's metrics service port                                                                                                                                                                                           | `8443`                                                                                                            |
| `metricsService.type`                       | Operator's metrics service type                                                                                                                                                                                           | `ClusterIP`                                                                                                       |
| `webhookService.labels`                     | Labels for the operator's webhook service                                                                                                                                                                                 | `{}` (nil)                                                                                                        |
| `webhookService.annotations`                | Annotations for the operator's webhook service                                                                                                                                                                            | `{}` (nil)                                                                                                        |
| `webhookService.port`                       | Operator's webhook service port                                                                                                                                                                                           | `443`                                                                                                             |
| `webhookService.targetPort`                 | Operator's webhook target port                                                                                                                                                                                            | `9443`                                                                                                            |
| `webhookService.type`                       | Operator's webhook service type                                                                                                                                                                                           | `ClusterIP`                                                                                                       |
| `podSecurityContext`                        | Security context for the operator pods                                                                                                                                                                                    | `{}` (nil)                                                                                                        |
| `securityContext`                           | Security context for the operator container                                                                                                                                                                               | `allowPrivilegeEscalation: false` (see `values.yaml`)                                                             |
| `livenessProbe`                             | Liveliness probe for operator container                                                                                                                                                                                   | `initialDelaySeconds: 15`, `periodSeconds: 20`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3` |
| `readinessProbe`                            | Readiness probe for the operator container                                                                                                                                                                                | `initialDelaySeconds: 5`, `periodSeconds: 10`, `timeoutSeconds: 1`, `successThreshold: 1`, `failureThreshold: 3`  |

<!-- ## Next Steps

Deploy [Aerospike Cluster](https://artifacthub.io/packages/helm/aerospike/aerospike-cluster) -->
