---
title: Delete Aerospike Cluster
description: Delete Aerospike Cluster
---

You can delete a cluster either by using the customer resource file that you created the cluster with, or by deleting the cluster directly

## Delete a cluster

To delete a cluster, run the following command using the correct custom resource file:

```sh
$ kubectl delete -f deploy/samples/dim_nostorage_cluster_cr.yaml
```

