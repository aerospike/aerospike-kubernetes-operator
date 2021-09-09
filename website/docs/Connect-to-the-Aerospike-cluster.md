---
title: Connect To The Aerospike Cluster
description: Connect To The Aerospike Cluster
---

You can connect to an Aerospike cluster deployed by Aerospike Kubernetes Operator through asadm or through applications that use Aerospike client libraries. 

## Port access
When the Aerospike cluster is deployed in a single pod per Kubernetes host mode, ports `3000 (service port)` and `4333 (TLS port)` on all Kubernetes hosts should be accessible to all client and tools.

When the Aerospike cluster is configured to have multiple pods per Kubernetes host mode, port-range `(30000–32767)` on all Kubernetes hosts should be accessible to all client and tools.

Configure the firewall rules for the Kubernetes cluster accordingly.

Also see [Cluster-configuration-settings](Cluster-configuration-settings.md) file for the use of `multiPodPerHost` setting.

## Obtain the Aerospike node endpoints

Run the kubectl describe command to get the IP addresses and port numbers:

 kubectl -n <Kubernetes_namespace> describe aerospikecluster <Aerospike_cluster>


The **Status > Pods*** section provides pod-wise access, alternate access, TLS access, and TLS alternate access endpoints as well as TLS name (if TLS is configured) to be used to access the cluster.

```sh
$ kubectl -n aerospike describe aerospikecluster aerocluster
Name:         aerocluster
Namespace:    aerospike
Labels:       <none>
Annotations:  API Version:  aerospike.com/v1alpha1
API Version:  aerospike.com/v1alpha1
Kind:         AerospikeCluster
.
.
.
Status:
  Aerospike Access Control:
    Users:
      Name:  admin
      Roles:
        sys-admin
        user-admin
      Secret Name:  auth-secret
  Aerospike Config:
    Logging:
      Any:         info
      Clustering:  debug
      Name:        /var/log/aerospike/aerospike.log
      Any:         info
      Name:        console
    Namespaces:
      Memory - Size:         3000000000
      Name:                  test
      Replication - Factor:  2
      Storage - Engine:      memory
.
.
.
  Pods:
    aerocluster-0-0:
      Aerospike:
        Access Endpoints:
          10.128.15.225:31312
        Alternate Access Endpoints:
          34.70.193.192:31312
        Cluster Name:  aerocluster
        Node ID:       0a0
        Tls Access Endpoints:
        Tls Alternate Access Endpoints:
        Tls Name:
      Host External IP:  34.70.193.192
      Host Internal IP:  10.128.15.225
      Image:             aerospike/aerospike-server-enterprise:5.2.0.7
      Initialized Volume Paths:
        /opt/aerospike
      Pod IP:        10.0.4.6
      Pod Port:      3000
      Service Port:  31312
    aerocluster-0-1:
      Aerospike:
        Access Endpoints:
          10.128.15.226:30196
        Alternate Access Endpoints:
          35.192.88.52:30196
        Cluster Name:  aerocluster
        Node ID:       0a1
        Tls Access Endpoints:
        Tls Alternate Access Endpoints:
        Tls Name:
      Host External IP:  35.192.88.52
      Host Internal IP:  10.128.15.226
      Image:             aerospike/aerospike-server-enterprise:5.2.0.7
      Initialized Volume Paths:
        /opt/aerospike
      Pod IP:        10.0.5.8
      Pod Port:      3000
      Service Port:  30196

```

## Connecting to the cluster
When connecting from outside the Kubernetes cluster network, you need to use the host external IPs. By default, the Operator configures access endpoints to use Kubernetes host internal IPs and alternate access endpoints to use host external IPs.

Please refer to [network policy](Cluster-configuration-settings.md#network-policy) configuration for details.

From the example status output, for pod aerocluster-0-0, the alternate access endpoint is 34.70.193.192:31312

### With client
To use a client from outside the Kubernetes network using external IPs set the following for the client policy using appropriate client API.
```yaml
host: 34.70.193.192
port: :31312
username: admin
password: admin123 # based on the configured secret
use-services-alternate: true
```

To use asadm from within the Kubernetes network run
```yaml
host: 10.128.15.225
port: :31312
username: admin
password: admin123 # based on the configured secret
use-services-alternate: false
```

### With asadm
With kubectl
```sh
# kubectl run -it --rm --restart=Never aerospike-tool -n aerospike --image=aerospike/aerospike-tools:latest -- asadm -h <cluster-name> -U <user> -P <password>
kubectl run -it --rm --restart=Never aerospike-tool -n aerospike --image=aerospike/aerospike-tools:latest -- asadm -h aeroclustersrc -U admin -P admin123
```

To use asadm from outside the Kubernetes network:
```sh
$ asadm -h 34.70.193.192:31312 -U admin -P admin123 --services-alternate
```

To use asadm from within the Kubernetes network:
```sh
$ asadm -h 10.128.15.225:31312 -U admin -P admin123
```
