# Aerospike Restore (Custom Resource) Helm Chart

A Helm chart for `AerospikeRestore` custom resource to be used with the Aerospike Kubernetes Operator.

## Pre Requisites

- Kubernetes 1.19+
- Aerospike Kubernetes Operator

## Usage

### Add Helm Repository

```sh
helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-enterprise
helm repo update
```

### Create Aerospike Restore

#### Install the chart

`<namespace>` used to install Aerospike restore helm chart must be included in `watchNamespaces` value of
aerospike-kubernetes-operator's `values.yaml`

```sh
# helm install <chartName> <chartPath> --namespace <namespace>
helm install aerospike-restore aerospike/aerospike-restore
```

It is recommended to create a separate YAML file with configurations as per your requirements and use it
with `helm install`.

```sh
helm install aerospike-restore aerospike/aerospike-restore \
    -f <customized-values-yaml-file>
```

## Configurations

| Name                 | Description                                                          | Default    |
|----------------------|----------------------------------------------------------------------|------------|
| `customLabels`       | Custom labels to add on the AerospikeRestore resource                | `{}` (nil) |
| `backupService.name` | Aerospike backup service name                                        |            |
| `backupService.name` | Aerospike backup service namespace                                   |            |
| `type`               | Type of restore. It can be of type Full, Incremental, and Timestamp. | `Full`     |
| `restoreConfig`      | Aerospike restore configuration                                      | `{}` (nil) |
| `pollingPeriod`      | Polling period for restore operation status                          | `60s`      |

### Configurations Explained
Refer
to [AerospikeRestore Customer Resource Spec](https://aerospike.com/docs/cloud/kubernetes/operator/backup-and-restore/restore-configuration#spec)
for details on above [configuration fields](#Configurations)
