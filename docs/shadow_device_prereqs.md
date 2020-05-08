## Deploy an Aerospike cluster with namespace data storage on local SSDs and with Shadow Device configuration

### Description

- Deploying an Aerospike cluster with namespace `storage-engine` as `device` (raw block device mode).
- Use Aerospike **Shadow Device configuration** to guarantee persistence.

    In cloud environments, the direct-attached or local SSDs (also called as ephemeral drives/volumes) does not guarantee persistence. These volumes are created along with the instance, and purged when the instance stops. The local ephemeral volumes are much faster compared to persistent disks which are network attached (for example, EBS volumes on AWS). Aerospike allows the [configuration of shadow devices](https://www.aerospike.com/docs/operations/configure/namespace/storage/#recipe-for-shadow-device) where all the writes are also propagated to a secondary persistent storage device.

    ```sh
    storage-engine device{
            device /dev/nvme0n1 /dev/sdf
            device /dev/nvme0n2 /dev/sdg
            ...
    }
    ```
- Configure and deploy a local volume provisioner to manage local SSDs and automate volume provisioning for Aerospike pods.

Let's get started.

### Create discovery directory and link the devices

Before deploying local volume provisioner, create a discovery directory on each worker node and link the block devices to be used into the discovery directory. The provisioner will discover local block volumes from this directory.

In this example, there are two local SSDs (identified as `/dev/nvme0n1` and `/dev/nvme0n2`) attached to each worker node which can be used for the Aerospike Cluster deployment.

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

> Note : <br /> You can use also your own discovery directory, but make sure that the [provisioner](aerospike-local-volume-provisioner.yaml) is also configured to point to the same directory.

### Configure and deploy local volume provisioner

To automate the local volume provisioning, we will create and run a provisioner based on [kubernetes-sigs/sig-storage-local-static-provisioner](https://github.com/kubernetes-sigs/sig-storage-local-static-provisioner). 

The provisioner will run as a `DaemonSet` which will manage the local SSDs on each node based on a discovery directory, create/delete the PersistentVolumes and clean up the storage when it is released.

The local volume static provisioner for this example is defined in [aerospike_local_volume_provisioner.yaml](deploy/aerospike_local_volume_provisioner.yaml). Each specification is highlighted with comments in the same file.

Create local storage class and then Deploy the provisioner,

```sh
$ kubectl create -f deploy/local_storage_class.yaml

$ kubectl create -f deploy/aerospike_local_volume_provisioner.yaml
```

Verify the discovered and created PV objects,
```sh
$ kubectl get pv

NAME                CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS      CLAIM   STORAGECLASS     REASON   AGE
local-pv-342b45ed   375Gi      RWO            Delete           Available           "local-ssd"            3s
local-pv-3587dbec   375Gi      RWO            Delete           Available           "local-ssd"            3s
local-pv-df716a06   375Gi      RWO            Delete           Available           "local-ssd"            3s
local-pv-eaf4a027   375Gi      RWO            Delete           Available           "local-ssd"            3s
```

Note that the `storageclass` configured here is `"local-ssd"`. We will use this storageclass in PVC or volumeClaimTemplates to talk to the provisioner and request PV resources (See [Statefulset definition](#deploy-aerospike-cluster-using-a-statefulset-defintion)).
