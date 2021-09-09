---
title: Aerospike Kubernetes Operator
description: Learn about the Aerospike Kubernetes Operator.
slug: /
---

The Aerospike Kubernetes Operator automates the deployment and management of **Aerospike enterprise clusters** on Kubernetes. The Operator provides a controller that manages a [Custom Resource Definition (CRD)](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) to extend the Kubernetes API for Aerospike Enterprise clusters. Aerospike cluster deployment and life cycle management are performed by updating an Aerospike cluster Custom Resource (CR).

## What features does it provide?

The goal of the Operator is to give you the ability to deploy multi-node Aerospike clusters, recover automatically from node failures, scale up or down automatically as load changes, ensure nodes are evenly split across racks or zones, automatically update to new versions of Aerospike and manage configuration changes in your clusters.


The Operator supports the following capabilities:
 * Deploy Aerospike clusters
 * Scale up and down existing Aerospike clusters
 * Version upgrade and downgrade
 * Configure persistent storage and resource allocation
 * Standardize and validate configurations
 * Cluster security management


## Architecture

The Aerospike Kubernetes Operator has a custom controller (written in go) that allows us to embed specific lifecycle management logic to effectively manage the state of an Aerospike cluster.  It does so by managing a Custom Resource Definition (CRD) to extend the Kubernetes API for Aerospike clusters.  Regular maintenance to the Aerospike cluster deployment and management can be performed by updating an Aerospike cluster Custom Resource (CR).

The Operator is deployed with StatefulSet and operates as a headless service to handle the DNS resolution of pods in the deployment.  Kubernetes StatefulSets is the workload API object that is used to manage stateful applications.  It is important because it manages the deployment and scaling of a set of Pods, and provides guarantees about the ordering and uniqueness of these Pods (e.g. as unique addressable identities).

A layered approach is taken to orchestration which allows the Operator to manage Aerospike Cluster tasks outside of the Aerospike deployment.



## Get started
 * [System Requirements](System-Requirements.md)
 * [Install the Operator on Kubernetes](Install-the-Operator-on-Kubernetes.md)
 * [Create an Aerospike cluster](Create-Aerospike-cluster.md)

## See also
 * [Kubernetes](https://kubernetes.io)
 * [Limitations](Limitations.md)
