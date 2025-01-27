# Aerospike Backup (Custom Resource) Helm Chart

A Helm chart for `AerospikeBackup` custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.19+
- Aerospike Kubernetes Operator

## Usage

### Add Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-enterprise
helm repo update
```

### Create Aerospike Backup

#### Install the chart

`<namespace>` used to install Aerospike backup helm chart must be included in `watchNamespaces` value of
aerospike-kubernetes-operator's `values.yaml`

```sh
# helm install <chartName> <chartPath> --namespace <namespace>
helm install aerospike-backup aerospike/aerospike-backup
```

It is recommended to create a separate YAML file with configurations as per your requirements and use it
with `helm install`.

```sh
helm install aerospike-backup aerospike/aerospike-backup \
    -f <customized-values-yaml-file>
```

## Configurations

| Name                             | Description                                          | Default    |
|----------------------------------|------------------------------------------------------|------------|
| `customLabels`                   | Custom labels to add on the AerospikeBackup resource | `{}` (nil) |
| `backupService.name`             | Aerospike backup service name                        |            |
| `backupService.namespace`        | Aerospike backup service namespace                   |            |
| `backupConfig`                   | Aerospike backup configuration                       | `{}` (nil) |
| `onDemandBackups[*].id`          | Unique identifier for the on-demand backup           |            |
| `onDemandBackups[*].routineName` | Routine name used to trigger on-demand backup        |            |
| `onDemandBackups[*].delay`       | Delay interval before starting the on-demand backup  |            |

### Configurations Explained
Refer
to [AerospikeBackup Customer Resource Spec](https://aerospike.com/docs/cloud/kubernetes/operator/backup-and-restore/backup-configuration#spec)
for details on above [configuration fields](#Configurations)
