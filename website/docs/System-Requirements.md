---
title: System Requirements
description: System Requirements
---

## What does it support?

The Aerospike Operator deploys and manages Aerospike Database Enterprise Edition, versions 4.6.0 and later.  

The Operator is supported on the following platforms:

 * Kubernetes 1.16, 1.17, 1.18
 * Amazon Elastic Kubernetes Service 
 * Google Kubernetes Engine
 * Microsoft Azure Kubernetes Service

## How does it work?

The Aerospike Operator extends Kubernetes by defining types that represent Aerospike clusters. These types are declarative; they define what the cluster should look like. The Operator monitors Kubernetes for Aerospike resources, creating or updating Aerospike Clusters to match the defined specification. 

## Get started
 * [Install the Operator](Install-the-Operator-on-Kubernetes.md)
 * [Create the Aerospike cluster](Create-Aerospike-cluster.md)

## See also
 * [Kubernetes](https://kubernetes.io)
 * [Limitations](Limitations.md)