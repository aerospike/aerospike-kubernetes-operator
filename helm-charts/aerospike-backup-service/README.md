# Aerospike Backup Service (Custom Resource) Helm Chart

A Helm chart for `AerospikeBackupService` custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.19+
- Aerospike Kubernetes Operator

## Usage

### Add Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-enterprise
helm repo update
```

### Deploy Aerospike Backup Service

#### Install the chart

`<namespace>` used to install Aerospike backup service chart must be included in `watchNamespaces` value of
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

| Name                         | Description                                                                   | Default                              |
|------------------------------|-------------------------------------------------------------------------------|--------------------------------------|
| `image.repository`           | Aerospike backup service container image repository                           | `aerospike/aerospike-backup-service` |
| `image.tag`                  | Aerospike backup service container image tag                                  | `2.0.0`                              |
| `customLabels`               | Custom labels to add on the AerospikeBackupService resource                   | `{}` (nil)                           |
| `serviceAccount.create`      | Enable ServiceAccount creation for Aerospike backup service.                  | true                                 |
| `serviceAccount.annotations` | ServiceAccount annotations                                                    | `{}` (nil)                           |
| `backupServiceConfig`        | Aerospike backup service configuration                                        | `{}` (nil)                           |
| `secrets`                    | Secrets to be mounted in the Aerospike Backup Service pod like aws creds etc. | `[]` (nil)                           |
| `resources`                  | Aerospike backup service pod resource requirements                            | `{}` (nil)                           |
| `service`                    | Kubernetes service configuration for Aerospike backup service                 | `{}` (nil)                           |


### Configurations Explained

[//]: # (TODO: Update below link when the documentation is available.)
Refer
to [AerospikeBackupService Customer Resource Spec](https://docs.aerospike.com/cloud/kubernetes/operator/cluster-configuration-settings#spec)
for details on above [configuration fields](#Configurations)
