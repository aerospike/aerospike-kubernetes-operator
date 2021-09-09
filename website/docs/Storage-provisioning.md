---
title: Storage Provisioning
description: Storage Provisioning
---

You need to set up [storage classes](https://kubernetes.io/docs/concepts/storage/storage-classes/) to use persistent storage external to the containers. These storage classes are used for dynamically provisioning the persistent storage demanded by users in aerospike CR configuration. Persistent storage on the pods will use these storage class provisioners to provision storage.

The storage configuration depends on the environment the Kubernetes cluster is deployed. For e.g. different cloud providers supply their own implementations of storage provisioners that dynamically create and attach storage devices to the containers.

Before deploying an Aerospike cluster that uses persistent storage, you need to create a **storage-class.yaml**  file, that defines the storage classes and then apply them onto the Kubernetes cluster.


## Google cloud storage classes

The following **storage-class.yaml** file uses the Google Cloud GCE provisioner to create a storage class called **ssd**.

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ssd
provisioner: kubernetes.io/gce-pd
parameters:
  type: pd-ssd
```

## Local volume

In this example, there is a local SSD (identified as `/dev/nvme0n1`). This should be attached to each Kubernetes worker node which will be used for getting the primary storage device for Aerospike Cluster deployment.

### Create discovery directory and link the devices

Before deploying local volume provisioner, create a discovery directory on each worker node and link the block devices to be used in the discovery directory. The provisioner will discover local block volumes from this directory.

```
$ lsblk
NAME    MAJ:MIN RM  SIZE RO TYPE MOUNTPOINT
nvme0n1       8:16   0  375G  0 disk
nvme0n2       8:32   0  375G  0 disk
```

```sh
$ mkdir /mnt/disks
$ sudo ln -s /dev/nvme0n1 /mnt/disks/
$ sudo ln -s /dev/nvme0n2 /mnt/disks/
```

:::note
You can use also your own discovery directory, but make sure that the [provisioner](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/storage/aerospike_local_volume_provisioner.yaml) is also configured to point to the same directory.
:::

### Configure and deploy local volume provisioner

To automate the local volume provisioning, we will create and run a provisioner based on [kubernetes-sigs/sig-storage-local-static-provisioner](https://github.com/kubernetes-sigs/sig-storage-local-static-provisioner).

The provisioner will run as a `DaemonSet` which will manage the local SSDs on each node based on a discovery directory, create/delete the PersistentVolumes and clean up the storage when it is released.

The local volume static provisioner for this example is defined in [aerospike_local_volume_provisioner.yaml](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/storage/aerospike_local_volume_provisioner.yaml).

The storage class yaml is defined in [local_storage_class.yaml](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/storage/local_storage_class.yaml).

Create local storage class and then deploy the provisioner.

```sh
$ kubectl create -f deploy/samples/storage/local_storage_class.yaml

$ kubectl create -f deploy/samples/storage/aerospike_local_volume_provisioner.yaml
```

Verify the discovered and created PV objects,
```sh
$ kubectl get pv

NAME                CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS      CLAIM   STORAGECLASS     REASON   AGE
local-pv-342b45ed   375Gi      RWO            Delete           Available           "local-ssd"            3s
local-pv-3587dbec   375Gi      RWO            Delete           Available           "local-ssd"            3s
```

:::note
The `storageclass` configured here is `"local-ssd"`. We will provide this in the Aerospike cluster CR config. This storageclass will be used to talk to the provisioner and request PV resources for the cluster.
:::
