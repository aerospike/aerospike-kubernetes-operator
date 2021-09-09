---
title: Limitations
description: Limitations
---
## General

The following restrictions are currently in place and apply to any cluster managed by the operator.

* aerospikeConfig cannot be empty
* aerospikeConfig.namespace cannot be nil or empty
* ca-path in TLS config not allowed, Only ca-file is allowed
* Strong consistency mode not yet supported
* Warm restart not yet supported
* All flash not yet supported

## When updating a cluster

The following restrictions apply to an already deployed cluster:

:::note
Although they can't be updated in place, BlockStorage, FileStorage, MultiPodPerHost, and Namespace storage can be adjusted using the [workaround described here](Scaling-namespace-storage.md).
:::

* Storage.Volumes config cannot be updated
* MultiPodPerHost cannot be updated
* Cluster security config flag "enable-security" cannot be updated after the first deployment
* TLS config cannot be updated
* aerospikeConfig.namespace.replication-factor cannot be updated
* Persistent Aerospike namespace cannot be added dynamically
* Namespace storage device cannot be resized. No new storage device can be added

These values cannot be given in aerospikeConfig in yaml config file. These are fixed or determined at runtime.

* service.node-id
* service.cluster-name
* network.service.port
* network.service.access-port
* network.service.alternate-access-port
* network.service.alternate-access-address
* network.service.tls-port
* network.service.tls-access-port
* network.service.tls-access-address
* network.service.tls-alternate-access-port
* network.service.tls-alternate-access-address
* network.heartbeat.mode
* network.heartbeat.port
* network.heartbeat.tls-port
* network.fabric.port
* network.fabric.tls-port
