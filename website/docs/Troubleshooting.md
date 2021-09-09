---
title: Troubleshooting
description: Troubleshooting
---

- [Pods stuck in pending state](Troubleshooting.md#pods-stuck-in-pending-state)
- [Pods keep crashing](Troubleshooting.md#pods-keep-crashing)
- [Error connecting to the cluster from outside Kubernetes](Troubleshooting.md#error-connecting-to-the-cluster-from-outside-kubernetes)
- [Troubleshooting](Troubleshooting.md#troubleshooting)
  * [Events](Troubleshooting.md#events)
  * [Operator logs](Troubleshooting.md#operator-logs)

## Pods stuck in pending state
After an Aerospike cluster has been created or updated if the pods are stuck with "Pending" status like so,

```sh
NAME          READY   STATUS      RESTARTS   AGE
aerocluster-0-0     1/1     Pending     0          48s
aerocluster-0-1     1/1     Pending     0          48s
```

describe the pod to find the reason for scheduling failure.

```sh
kubectl -n aerospike describe pod aerocluster-0-0
```

Under the events section you will find the reason for the pod not being scheduled. For example
```sh
QoS Class:       Burstable
Node-Selectors:  <none>
Tolerations:     node.kubernetes.io/not-ready:NoExecute op=Exists for 300s
                 node.kubernetes.io/unreachable:NoExecute op=Exists for 300s
Events:
  Type     Reason            Age                    From               Message
  ----     ------            ----                   ----               -------
  Warning  FailedScheduling  9m27s (x3 over 9m31s)  default-scheduler  0/1 nodes are available: 1 pod has unbound immediate PersistentVolumeClaims.
  Warning  FailedScheduling  20s (x9 over 9m23s)    default-scheduler  0/1 nodes are available: 1 node(s) didn't match Pod's node affinity.

```

Possible reasons are
 * Storage class incorrect or not created. Please see [persistent storage](Create-Aerospike-cluster.md#configure-persistent-storage) configuration for details.
 * 1 node(s) didn't match Pod's node affinity - Invalid zone, region, racklabel etc. for the rack configured for this pod.
 * Insufficient resources, CPU or memory available to schedule more pods.

## Pods keep crashing
After an Aerospike cluster has been created or updated if the pods are stuck with "Error" or "CrashLoopBackOff" status like so,

```sh
NAME          READY   STATUS      RESTARTS   AGE
aerocluster-0-0     1/1     Error     0          48s
aerocluster-0-1     1/1     CrashLoopBackOff     2          48s
```

Check the following logs to see if pod initialization failed or the Aerospike Server stopped.

Init logs
```sh
kubectl -n aerospike logs aerocluster-0-0 -c aerospike-init
```

Server logs
```sh
kubectl -n aerospike logs aerocluster-0-0 -c aerospike-server
```

Possible reasons are
 * Missing or incorrect feature key file - Fix by deleting the Aerospike secret and recreating it with correct feature key file. See [Aerospike secrets](Create-Aerospike-cluster.md#create-secrets) for details.
 * Bad Aerospike configuration - The operator tries to validate the configuration before applying it to the cluster. However it's still possible to misconfigure the Aerospike server. The offending paramter will be logged in the server logs and should be fixed and applied again to the cluster. See [Aerospike configuration change](Aerospike-configuration-change.md) for details.

## Error connecting to the cluster from outside Kubernetes

If the cluster runs fine as verfied by the pod status and asadm (see connecting with [asadm](Connect-to-the-Aerospike-cluster.md#with-asadm)), Ensure that firewall allows inbound traffic to the Kubenetes cluster for the Aerospike ports. See [Port access](Connect-to-the-Aerospike-cluster.md#port-access) for details.

## Troubleshooting
### Events
Events (High-level info on what is happening in the cluster) (scheduling failure, storage unavailable etc.)

```sh
kubectl -n aerospike get events
```

### Operator logs
List the operator instance and select the operator instance with READY column 1/1. This is the active operator instance even if you run multiple replicas for high availability.

```sh
kubectl  -n aerospike get pod

NAME                                             READY   STATUS    RESTARTS   AGE
aerocluster-0-0                                  2/2     Running   0          133m
aerocluster-0-1                                  2/2     Running   0          133m
aerocluster-0-2                                  2/2     Running   0          12m
aerospike-kubernetes-operator-5f44549fc9-hwk8k   0/1     Running   0          15m
aerospike-kubernetes-operator-5f44549fc9-n7vj2   1/1     Running   0          15m
```

Get the log

```sh
kubectl -n aerospike logs aerospike-kubernetes-operator-5f44549fc9-n7vj2
```

Add the -f flag to follow the logs continuously.

```sh
kubectl -n aerospike logs -f aerospike-kubernetes-operator-5f44549fc9-n7vj2
```

The series of steps the operator follows to apply user changes are logged allow with debug information, errors, and warnings.
