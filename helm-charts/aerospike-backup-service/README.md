# Aerospike Backup Service (Custom Resource) Helm Chart

A Helm chart for `AerospikeBackupService` (ABS) custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.23+
- Aerospike Kubernetes Operator

## Usage

### Add Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-enterprise
helm repo update
```

### Deploy Aerospike Backup Service

#### Prepare the namespace

We recommend using one ABS deployment per Aerospike cluster.

Create the service account for the ABS in the namespace where ABS is deployed

```sh
kubectl create serviceaccount aerospike-backup-service -n <namespace>
```

> Note: ServiceAccount name can be configured. Update the configured ServiceAccount in a ABS CR file to use a different service account name.

#### Install the chart

`<namespace>` used to install ABS chart must be included in `watchNamespaces` value of
aerospike-kubernetes-operator's `values.yaml`

```sh
# helm install <chartName> <chartPath> --namespace <namespace>
helm install aerospike-backup-service aerospike/aerospike-backup-service
```

It is recommended to create a separate YAML file with configurations as per your requirements and use it
with `helm install`.

```sh
helm install aerospike-backup-service aerospike/aerospike-backup-service \
    -f <customized-values-yaml-file>
```

## Configurations

| Name                  | Description                                                                   | Default                              |
|-----------------------|-------------------------------------------------------------------------------|--------------------------------------|
| `image.repository`    | Aerospike backup service container image repository                           | `aerospike/aerospike-backup-service` |
| `image.tag`           | Aerospike backup service container image tag                                  | `3.1.0`                              |
| `customLabels`        | Custom labels to add on the Aerospike backup service resource                 | `{}` (nil)                           |
| `backupServiceConfig` | Aerospike backup service configuration                                        | `{}` (nil)                           |
| `secrets`             | Secrets to be mounted in the Aerospike backup service pod like aws creds etc. | `[]` (nil)                           |
| `resources`           | Aerospike backup service pod resource requirements                            | `{}` (nil)                           |
| `service`             | Kubernetes service configuration for Aerospike backup service                 | `{}` (nil)                           |
| `podSpec`             | Aerospike backup service pod configuration                                    | `{}` (nil)                           |

### Configurations Explained
Refer
to [AerospikeBackupService Customer Resource Spec](https://aerospike.com/docs/cloud/kubernetes/operator/backup-and-restore/backup-service-configuration#spec)
for details on above [configuration fields](#Configurations)
