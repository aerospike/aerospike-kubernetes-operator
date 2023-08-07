# Aerospike Kubernetes Operator

## Overview

The Aerospike Kubernetes Operator automates the deployment and management of **Aerospike enterprise clusters** on
Kubernetes. The Operator provides a controller that manages
a [Custom Resource Definition (CRD)](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
to extend the Kubernetes API for Aerospike Enterprise clusters. Aerospike cluster deployment and life cycle management
are performed by updating an Aerospike cluster Custom Resource (CR).

## Documentation

For full documentation please visit the **[official documentation](https://docs.aerospike.com/cloud/kubernetes/operator).**

## What features does it provide?

The goal of the Operator is to give you the ability to deploy multi-node Aerospike clusters, recover automatically from
node failures, scale up or down automatically as load changes, ensure nodes are evenly split across racks or zones,
automatically update to new versions of Aerospike and manage configuration changes in your clusters.

The Operator supports the following capabilities:

* Deploy Aerospike clusters
* Scale up and down existing Aerospike clusters
* Version upgrade and downgrade
* Configure persistent storage and resource allocation
* Standardize and validate configurations
* Cluster security management

## Building and quick start

### Generate CRD manifests

```sh
make generate
make manifests
```

### Build and push operator image

Run the following command with the appropriate name and version for the operator's image.

```sh
IMAGE_TAG_BASE=aerospike/aerospike-kubernetes-operator-nightly
VERSION=3.0.0
make docker-buildx IMG=${IMAGE_TAG_BASE}:${VERSION} PLATFORMS=linux/amd64
```
**Note**: Change `PLATFORMS` var as per host machine or remove it to build multi-arch image

### Developer testing

You can use the following for quickly trying out the operator without
using [OLM](https://github.com/operator-framework/operator-lifecycle-manager/).

#### Deploy

Make sure cert-manager is deployed on your Kubernetes cluster using
instructions [here](https://cert-manager.io/docs/installation/kubernetes/).

To deploy the operator build in the previous step run

```sh
make deploy IMG=${IMAGE_TAG_BASE}:${VERSION}
```

### Undeploy

To undeploy run

```sh
make undeploy IMG=${IMAGE_TAG_BASE}:${VERSION}
```

**Note**: This will also delete the deployed Aerospike clusters because the CRD definitions and all operator related
objects are deleted.

## Operator Lifecycle Manager (OLM) integration bundle

Operator Lifecycle Manager (OLM) is a tool to help manage the Operators running on your cluster. This is the preferred
way to manage Kubernetes operators in production. This section describes how to generate the OLM bundle and run the
operator using OLM.

### Install operator-sdk

Install operator-sdk version 1.28.0 using the
installation [guide](https://v1-28-x.sdk.operatorframework.io/docs/installation/)

### Build the bundle

Make sure the operator's image has also been [pushed](#build-and-push-operator-image).

Set up the environment with image names.

```shell
export ACCOUNT=aerospike
export IMAGE_TAG_BASE=${ACCOUNT}/aerospike-kubernetes-operator
export VERSION=3.0.0
export IMG=docker.io/${IMAGE_TAG_BASE}-nightly:${VERSION}
export BUNDLE_IMG=docker.io/${IMAGE_TAG_BASE}-bundle-nightly:${VERSION}
export CATALOG_IMG=docker.io/${IMAGE_TAG_BASE}-catalog-nightly:${VERSION}
```

Create the bundle

```shell
make bundle
```

### Build bundle image and publish

```shell
make bundle-build bundle-push
```

### Build catalog image and publish

```shell
make docker-buildx-catalog
```

### Deploy operator with OLM

Install OLM if not already done

```shell
operator-sdk olm install
```

Create **aerospike** namespace if it does not exist

```shell
kubectl create namespace aerospike
```

### Deploy the operator using custom CatalogSource:

#### Deploy custom CatalogSource:

```shell
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: aerospike
  namespace: aerospike
spec:
  displayName: Aerospike operator
  publisher: Aerospike operator
  sourceType: grpc
  image: "${CATALOG_IMG}"
  updateStrategy:
    registryPoll:
      interval: 10m
EOF
```

#### Create Operator Group for the targeted namespaces:

- Targeting single namespace
```shell
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: test-operator-group
  namespace: test
spec:
  targetNamespaces:
    - aerospike
  upgradeStrategy: Default
EOF
```

- Targeting multiple namespaces
  Assuming you want the operator to target two other namespaces ns1 and ns2, create operator group with MultiNamespace install mode.
```shell
```shell
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: test-operator-group
  namespace: test
spec:
  targetNamespaces:
    - ns1
    - ns2
  upgradeStrategy: Default
EOF
```

#### Create Subscription to deploy operator:
```shell
kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: aerospike-kubernetes-operator
  namespace: test
spec:
  channel: stable
  installPlanApproval: Automatic
  name: aerospike-kubernetes-operator
  source: aerospike
  sourceNamespace: aerospike
EOF
```

### Deploy your Aerospike clusters

Deploy Aerospike clusters using the Operator
documentation [here](https://docs.aerospike.com/docs/cloud/kubernetes/operator/create-cluster-kubectl.html).

### Undeploy operator with OLM

```shell
operator-sdk cleanup aerospike-kubernetes-operator --namespace=aerospike
```

### Running tests

The operator tests require following prerequisites

- A running Kubernetes cluster with at least 3 nodes with at least 12 CPUs
- A storage class named "ssd" that allows provisioning of filesystem and block devices
- The `kubectl` command configured to use the above cluster
- OLM bundle image created with as described [here](#build-the-bundle)
- No production services should be running on this cluster - including the operator or production Aerospike clusters

The operator tests create and use 4 namespaces

- test
- test1
- test2
- aerospike

Run the entire test suite

```shell
./test/test.sh -b $BUNDLE_IMG -c $CATALOG_IMG
```

Run tests matching a regex

```shell
./test/test.sh -b $BUNDLE_IMG -c $CATALOG_IMG '-ginkgo.focus=".*MultiCluster.*"'
```

## Architecture

The Aerospike Kubernetes Operator has a custom controller (written in go) that allows us to embed specific lifecycle
management logic to effectively manage the state of an Aerospike cluster. It does so by managing a Custom Resource
Definition (CRD) to extend the Kubernetes API for Aerospike clusters. Regular maintenance to the Aerospike cluster
deployment and management can be performed by updating an Aerospike cluster Custom Resource (CR).

The Operator is deployed with StatefulSet and operates as a headless service to handle the DNS resolution of pods in the
deployment. Kubernetes StatefulSets is the workload API object that is used to manage stateful applications. It is
important because it manages the deployment and scaling of a set of Pods, and provides guarantees about the ordering and
uniqueness of these Pods (e.g. as unique addressable identities).

A layered approach is taken to orchestration which allows the Operator to manage Aerospike Cluster tasks outside the
Aerospike deployment.

## See also

* [Kubernetes](https://kubernetes.io)
