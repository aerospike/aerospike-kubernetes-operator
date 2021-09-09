---
title: Multiple Aerospike Clusters
description: Multiple Aerospike Clusters
---

The operator is able to deploy multiple Aerospike clusters within a single k8s namespace or in multiple k8s namespaces. The operator can watch all the namespaces specified in its yaml file and reconcile clusters deployed in them.

## Multiple Aerospike clusters in a single k8s namespace

Deploying multiple clusters in a single namespace is as easy as deploying a single cluster. The user has to just deploy another cluster with a cluster name (cluster object metadata name in cr.yaml file) that is not already registered in that namespace.

## Multiple Aerospike clusters in multiple k8s namespaces

Deploying multiple clusters in multiple namespaces require few steps to be followed

### Step 1

Add a list of namespaces to be watched by Operator in `operator.yaml` file.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aerospike-kubernetes-operator
  namespace: aerospike
spec:
    .
    .
    spec:
      containers:
        .
        .
        - name: aerospike-kubernetes-operator
          env:
          - name: WATCH_NAMESPACE
            # Use below value for watching multiple namespaces by the operator
            value: aerospike,aerospike1,aerospike2
```

### Step 2

Add a new Service account and a new entry for this Service account in ClusterRoleBinding for every namespace to be watched in `rbac.yaml` file.

```yaml

---
# Service account used by the cluster pods to obtain pod metadata.
apiVersion: v1
kind: ServiceAccount
metadata:
  # Do not change the name, its hard-coded in the operator
  name: aerospike-cluster
  namespace: aerospike

# Uncomment below service accounts for deploying clusters in additional namespaces
---
# Service account used by the cluster pods to obtain pod metadata.
apiVersion: v1
kind: ServiceAccount
metadata:
  # Do not change name, its hard-coded in operator
  name: aerospike-cluster
  namespace: aerospike1

---
# Role
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: aerospike-cluster
rules:
- apiGroups:
  - ""
  resources:
  - nodes
  - services
  verbs:
  - get
  - list
- apiGroups:
  - aerospike.com
  resources:
  - '*'
  verbs:
  - '*'

---
# RoleBinding
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: aerospike-cluster
roleRef:
  kind: ClusterRole
  name: aerospike-cluster
  apiGroup: rbac.authorization.k8s.io
subjects:
- kind: ServiceAccount
  name: aerospike-cluster
  namespace: aerospike
- kind: ServiceAccount
  name: aerospike-cluster
  namespace: aerospike1

```

### Step 3

Now deploy a new cluster in any of the watched namespaces using a `cr.yaml` file.

## XDR setup using Multicluster feature

Deploy XDR destination cluster using this [cr.yaml](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/xdr_dst_cluster_cr.yaml).

```sh
kubectl apply -f deploy/samples/xdr_dst_cluster_cr.yaml
```

Deploy XDR source cluster using this [cr.yaml](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/xdr_src_cluster_cr.yaml).

```sh
kubectl apply -f deploy/samples/xdr_src_cluster_cr.yaml
```

Here Source and Destination clusters are deployed in a single namespace. If the user wants to deploy these clusters in different namespaces then the user has to follow [these](Multiple-Aerospike-clusters.md#multiple-aerospike-clusters-in-multiple-k8s-namespaces) steps.
