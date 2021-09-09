---
title: Install The Operator On Kubernetes
description: Install The Operator On Kubernetes
---


## Create a Kubernetes cluster

To use the Aerospike Kubernetes Operator, you will need a working Kubernetes cluster with version 1.16, 1.17 or 1.18.

### Production Kubernetes Cluster

If you need to set up a new cluster: [See here for official guides](https://kubernetes.io/docs/setup/production-environment/)

There are specific guides for:

* [Amazon EKS](https://docs.aws.amazon.com/eks/latest/userguide/create-cluster.html)
* [Google GKE](https://cloud.google.com/kubernetes-engine/docs/how-to/creating-a-zonal-cluster)
* [Microsoft AKS](https://docs.microsoft.com/en-us/azure/aks/tutorial-kubernetes-deploy-cluster)

### Development/test Cluster

For prototyping and testing it is useful to use a local Kubernetes deployment.

The ones we have used include:

* [MicroK8s](https://microk8s.io/)
* [Minikube](https://github.com/kubernetes/minikube)

## Obtain the prerequisite files

Download the Aerospike Operator package [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/), and unpack it on the same computer where you normally run kubectl. The Operator package contains contains the CRDs and other resource files necessary to deploy the operator along with sample Aerospike cluster deployment resource files.

To clone the Aerospike Github Operator repository:

```sh
$ git clone https://github.com/aerospike/aerospike-kubernetes-operator.git
$ cd aerospike-kubernetes-operator
$ git checkout 1.0.1
```

The deploy folder has the prerequisite files.

## Create a new Kubernetes namespace

Create a new Kubernetes namespace for Aerospike. This will help in putting all Aerospike related resource in a single logical space.

```sh
$ kubectl create namespace aerospike
```

## Register Aerospike CRDs

Use the [aerospike.com_aerospikeclusters_crd.yaml](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/crds/aerospike.com_aerospikeclusters_crd.yaml) file to register the operator's CRDs.

```sh
$ kubectl apply -f deploy/crds/aerospike.com_aerospikeclusters_crd.yaml
```

## Setup RBAC

Setup [Role based access control (RBAC)](https://kubernetes.io/docs/reference/access-authn-authz/rbac/). RBAC helps in regulating access to the Kubernetes cluster and its resources based on the roles of individual users within your organization.

```sh
$ kubectl apply -f deploy/rbac.yaml
```

## Deploy the Aerospike Operator

### Standalone mode
Aerospike Kubernetes Operator can be deployed standalone by applying [deploy/operator.yaml](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/operator.yaml) file. This file has a deployment object containing the operator specs. This object can be modified to change specs, e.g. log level for the operator, and imagePullPolicy.

### HA mode
For the high availability of the operator `spec.replicas` can be set to more than 1 in `.yaml`. The Operator will automatically elect a leader among all replicas. You need to add a readiness probe which ensures that the Kubernetes service uses to the currently elected leader instance of the operator. Replace deploy/operator.yaml file with the following content to run 3 replicas for the operator. Please update `pec.replicas` to the desired replica count.

```yaml
# Service for webhook
apiVersion: v1
kind: Service
metadata:
  name: aerospike-cluster-webhook
  namespace: aerospike
spec:
  selector:
    # Specified by the deployment/pod
    name: aerospike-kubernetes-operator
  ports:
    - port: 443
      # Can be the name of port 8443 of the container
      targetPort: 8443
---

# Operator
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aerospike-kubernetes-operator
  namespace: aerospike
spec:
  # Number of operator replicas to run.
  replicas: 3
  selector:
    matchLabels:
      name: aerospike-kubernetes-operator
  template:
    metadata:
      labels:
        name: aerospike-kubernetes-operator
    spec:
      serviceAccountName: aerospike-kubernetes-operator
      containers:
        - name: aerospike-kubernetes-operator
          image: aerospike/aerospike-kubernetes-operator:1.0.1
          command:
          - aerospike-kubernetes-operator
          imagePullPolicy: Always
          ports:
          - containerPort: 8443
          env:
          - name: WATCH_NAMESPACE
            value: aerospike
            # Use below value for watching multiple namespaces by operator
            # value: aerospike,aerospike1,aerospike2
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: OPERATOR_NAME
            value: "aerospike-kubernetes-operator"
          - name: LOG_LEVEL
            value: debug
          readinessProbe:
            exec:
              command:
              - stat
              - "/tmp/cert"
            initialDelaySeconds: 5
            periodSeconds: 5
            timeoutSeconds: 5
            failureThreshold: 1
```


### Deploy

Once you have the  deploy/operator.yaml file deploy the operator using the following commands.
```sh
$ kubectl apply -f deploy/operator.yaml
```

## Verify Operator is running

```sh
$ kubectl get pod -n aerospike
```

```
NAME                                             READY   STATUS    RESTARTS   AGE
aerospike-kubernetes-operator-5587bc7758-psn5t   1/1     Running   0          70s
```

This step could take some time initially as the operator image needs to be downloaded the first time.

## Check Operator logs

Use the pod name obtained above to check the Operator logs.
```sh
$ kubectl -n aerospike logs -f aerospike-kubernetes-operator-5587bc7758-psn5t
```
```
t=2020-03-26T06:23:42+0000 lvl=info msg="Operator Version: 0.0.1" module=cmd caller=main.go:79
t=2020-03-26T06:23:42+0000 lvl=info msg="Go Version: go1.13.4" module=cmd caller=main.go:80
t=2020-03-26T06:23:42+0000 lvl=info msg="Go OS/Arch: linux/amd64" module=cmd caller=main.go:81
t=2020-03-26T06:23:42+0000 lvl=info msg="Version of operator-sdk: v0.12.0+git" module=cmd caller=main.go:82
t=2020-03-26T06:23:43+0000 lvl=info msg="Set sync period" module=cmd period=nil caller=main.go:183
t=2020-03-26T06:23:43+0000 lvl=info msg="Registering Components" module=cmd caller=main.go:199
....
```

## Next
 - [Create the Aerospike cluster](Create-Aerospike-cluster.md)
 - [Cluster configuration settings](Cluster-configuration-settings.md)
