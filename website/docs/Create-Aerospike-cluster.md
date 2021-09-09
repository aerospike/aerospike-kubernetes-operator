---
title: Create Aerospike Cluster
description: Create Aerospike Cluster
---

To deploy an Aerospike cluster using the Operator, you will create an Aerospike custom resource file that describes what you want the cluster to look like (e.g. number of nodes, types of services, system resources, etc), and then push that configuration file into Kubernetes.

## Prerequisites

Before deploying your Aerospike cluster ensure that you have:

- Reviewed the prerequisites and system requirements
- Downloaded the Aerospike Kubernetes Operator
- Installed the Operator on Kubernetes, and ensure that it is up and running

## Prepare the Aerospike cluster configuration:

The Operator package contains example YAML configuration files for the cluster deployment. After unpacking the files, the resulting directory will be /aerospike-kubernetes-operator/deploy.  Make sure to cd into this directory before you run the commands.

The use case for your cluster will help you to determine which configuration parameters that you need to set in the custom resource [(CR)](https://github.com/aerospike/aerospike-kubernetes-operator/wiki/Configuration) file. Identify your requirements for storage, if you plan to [enable XDR](XDR.md), or [manage TLS certificates](Manage-TLS-Certificates.md) for network security with your Aerospike clusters.

## Configure persistent storage

The Aerospike Operator is designed to work with dynamically provisioned storage classes. A [storage class](https://kubernetes.io/docs/concepts/storage/storage-classes/) is used to dynamically provision the persistent storage. Aerospike Server pods may have different storage volumes associated with each service.

To learn more about configuring persistent storage:
* For Amazon Elastic Kubernetes Service, the instructions are [here](https://docs.aws.amazon.com/eks/latest/userguide/storage-classes.html).
* For Google Kubernetes Engine, the instructions are [here](https://cloud.google.com/kubernetes-engine/docs/how-to/persistent-volumes/ssd-pd)
* For Microsoft Azure Kubernetes Service, the instructions are [here](https://docs.microsoft.com/en-us/azure/aks/azure-disks-dynamic-pv).

Persistent storage on the pods will use these storage class provisioners to provision storage.

To apply a sample storage class based on your Kubernetes environment:

For GKE
```sh
$ kubectl apply -f deploy/samples/storage/gce_ssd_storage_class.yaml
```

For EKS
```sh
$ kubectl apply -f deploy/samples/storage/eks_ssd_storage_class.yaml
```

For MicroK8s
```sh
$ kubectl apply -f deploy/samples/storage/microk8s_filesystem_storage_class.yaml
```

See [Storage Provisioning](Storage-provisioning.md) for more details on configuring persistent storage.

## Create secrets
Create secrets to setup Aerospike authentication, TLS, and features.conf. See [Manage-TLS-Certificates](Manage-TLS-Certificates.md) for more details.

Aerospike secrets like TLS certificates, security credentials, and features.conf can be packaged in a single directory and converted to Kubernetes secrets like so

```sh
$ kubectl  -n aerospike create secret generic aerospike-secret --from-file=deploy/secrets
```

Create a secret containing the password for Aerospike cluster admin user by passing the password from the command line.
```sh
$ kubectl  -n aerospike create secret generic auth-secret --from-literal=password='admin123'
```

## Create Aerospike cluster Custom Resource (CR)

Refer to the [cluster configuration settings](Cluster-configuration-settings.md) section for details on the Aerospike cluster custom resource (CR) file. Sample Aerospike cluster CR files for different configurations can be found [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.0/deploy/samples/).

This custom resource file can be edited later on to make any changes/manage the Aerospike cluster.


## Deploy Aerospike cluster

Use the CR yaml file that you created to deploy an Aerospike cluster.
```sh
$ kubectl apply -f deploy/samples/dim_nostorage_cluster_cr.yaml
```

:::note
Replace the file name with CR yaml file for your cluster.
:::

## Verify cluster status
Ensure that the aerospike-kubernetes-operator creates a [StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) for the CR.

```sh
$ kubectl get statefulset -n aerospike
NAME      READY   AGE
aerocluster-0   2/2     24s
```

Check the pods to confirm the status. This step may take time as the pod's provision resources, initialize, and are ready. Please wait for the pods to switch to the running state.

```sh
$ kubectl get pods -n aerospike
NAME          READY   STATUS      RESTARTS   AGE
aerocluster-0-0     1/1     Running     0          48s
aerocluster-0-1     1/1     Running     0          48s
```

If the Aerospike cluster pods do not switch to Running status in a few minutes, please refer to [Troubleshooting](Troubleshooting.md) guide.

## Next
- [Cluster configuration settings](Cluster-configuration-settings.md)
- [Connect to the Aerospike cluster](Connect-to-the-Aerospike-cluster.md)
