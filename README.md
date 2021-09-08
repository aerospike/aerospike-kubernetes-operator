# Aerospike Kubernetes Operator

## Overview

The Aerospike Kubernetes Operator automates the deployment and management of **Aerospike enterprise clusters** on Kubernetes. The Operator provides a controller that manages a [Custom Resource Definition (CRD)](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) to extend the Kubernetes API for Aerospike Enterprise clusters. Aerospike cluster deployment and life cycle management are performed by updating an Aerospike cluster Custom Resource (CR).

## Documentation

For full documentation please visit the **[official documentation](https://www.aerospike.com/docs/cloud/index.html).**

## What features does it provide?

The goal of the Operator is to give you the ability to deploy multi-node Aerospike clusters, recover automatically from node failures, scale up or down automatically as load changes, ensure nodes are evenly split across racks or zones, automatically update to new versions of Aerospike and manage configuration changes in your clusters.


The Operator supports the following capabilities:
 * Deploy Aerospike clusters
 * Scale up and down existing Aerospike clusters
 * Version upgrade and downgrade
 * Configure persistent storage and resource allocation
 * Standardize and validate configurations
 * Cluster security management

## Building

### Generate CRD manifests

```sh
make generate
make manifests
```

### Build and push operator's image

Run the following command with the appropriate name and version for the operator's image.

```sh
IMAGE_TAG_BASE=aerospike/aerospike-kubernetes-operator
VERSION=2.0.0-dev
make docker-build docker-push IMG=${IMAGE_TAG_BASE}:${VERSION}
```

### Deploy
Make sure cert-manager is deployed on your Kubernetes cluster using instructions [here](https://cert-manager.io/docs/installation/kubernetes/).

To deploy the operator build in the previous step run

```sh
make deploy IMG=${IMAGE_TAG_BASE}:${VERSION}
```

Setup RBAC

```sh
kubectl apply -f cluster_rbac.yaml
```

## Undeploy

Run

```sh
make undeploy IMG=${IMAGE_TAG_BASE}:${VERSION}
```

## Architecture

The Aerospike Kubernetes Operator has a custom controller (written in go) that allows us to embed specific lifecycle management logic to effectively manage the state of an Aerospike cluster.  It does so by managing a Custom Resource Definition (CRD) to extend the Kubernetes API for Aerospike clusters.  Regular maintenance to the Aerospike cluster deployment and management can be performed by updating an Aerospike cluster Custom Resource (CR).

The Operator is deployed with StatefulSet and operates as a headless service to handle the DNS resolution of pods in the deployment.  Kubernetes StatefulSets is the workload API object that is used to manage stateful applications.  It is important because it manages the deployment and scaling of a set of Pods, and provides guarantees about the ordering and uniqueness of these Pods (e.g. as unique addressable identities).

A layered approach is taken to orchestration which allows the Operator to manage Aerospike Cluster tasks outside of the Aerospike deployment.

## Get started
 * [System Requirements](https://www.aerospike.com/docs/cloud/System-Requirements.html)
 * [Install the Operator on Kubernetes](https://www.aerospike.com/docs/cloud/Install-the-Operator-on-Kubernetes.html)
 * [Create an Aerospike cluster](https://www.aerospike.com/docs/cloud/Create-Aerospike-cluster.html)
 * [Limitations](https://www.aerospike.com/docs/cloud/Limitations.html)

## See also
 * [Kubernetes](https://kubernetes.io)
