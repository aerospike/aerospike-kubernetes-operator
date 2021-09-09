---
slug: introducing-skyhook
title: Introducing Skyhook
tags: [skyhook, redis, aerospike]
---

> *https://dev.to/aerospike/skyhook-a-redis-compatible-interface-to-aerospike-database-4hjj*

[Aerospike](https://www.aerospike.com/) is a highly available and scalable NoSQL distributed database used in production to provide blazingly fast performance at Petabyte scale. Production deployments of Aerospike are almost always serving their data from NVMe drives, though it’s possible to run namespaces in memory.

Redis is a well loved key-value data store that is widely used by developers, which was designed to run single node (single-threaded originally) and in-memory.
This article might be interesting to developers who are using [Redis](https://redis.io/) and looking for more scalable and highly available alternatives, or maybe already have some applications running on Aerospike.

Migrating to a different technology is always a challenging process. You need to train engineers, rewrite the codebase, and set up a production cluster to take over. Using [Skyhook](https://github.com/aerospike/skyhook), you can move applications to Aerospike first, then come back to rewrite them as Aerospike native, or if you’re satisfied with their performance, keep them as they are.
Or maybe you’re looking to expose your Aerospike data to external Redis-based applications? We would love to hear how you would use this project.

## Skyhook to the rescue

We, as a company, observed this need and came up with the highly performant bridge service, which acts as a fully-fledged (with commands support limitation) Redis server, bridging the clients’ commands to Aerospike clusters.

## Technical details

Skyhook is a standalone server application written in Kotlin, which projects Redis protocol commands to an Aerospike cluster using the Aerospike Java client under the hood. The server supports a single namespace and set configuration, where the incoming commands will be applied. This project uses [Netty](https://netty.io/) as a non-blocking I/O client-server framework.

Netty, is a highly performant JVM asynchronous event-driven network application framework. Its customizable thread model and native transport support forge an incredibly performant network communication layer.

What’s left is to translate Redis commands to Aerospike commands, and here come the async capabilities of the Aerospike Java Client.

## Installation and configuration

You will need JDK8 and an Aerospike Server version >= 4.9 because Skyhook uses the new scan capabilities which are available starting from that version.

Clone [aerospike/skyhook](https://github.com/aerospike/skyhook) or grab a prebuilt executable JAR from the [releases](https://github.com/aerospike/skyhook/releases) to get started.

To build the service from the sources:

```sh
./gradlew clean build
```

This command will build an executable JAR file.

Skyhook is configured using a configuration file, and it’s in YAML format. You will need to specify it using the `-f` flag.

To run the server:

```sh
java -jar skyhook-[version]-all.jar -f config/server.yml
```

An example configuration file can be found in the config folder under the project repository.

Here are the current configuration properties available.

| Property name | Description | Default value |
| ------------- | ----------- | ------------- |
| hostList | The host list to seed the Aerospike cluster. | localhost:3000 |
| namespace | The Aerospike namespace. | test |
| set | The Aerospike set name. | redis |
| bin | The Aerospike bin name to set values. | b |
| redisPort | The server port to bind to. | 6379 |
| workerThreads<sup>1</sup> | The Netty worker group size. | number of available cores |
| bossThreads | The Netty acceptor group size. | 2 |

<sup>1</sup> `workerThreads` property is used to configure the size of the Aerospike Java Client EventLoops as well.

`workerThreads` and `bossThreads` are the Netty thread pool sizes and can be easily fine-tuned for optimal performance.
After the server is up and running, any Redis client can connect to it as if it were a regular Redis server.

For the test purposes you can use redis-cli or even the nc (or netcat) utility:

```sh
echo "GET key1\r\n" | nc localhost 6379
```

## Benchmarking

From the very beginning, the project was built with an attention to performance, and the results speak for themselves.

Running this Redis benchmark:

```sh
redis-benchmark -t set -r 100000 -n 1000000
```

We had the following results on local Skyhook with a single-node default configuration Aerospike cluster on Docker:

> *Summary:*  
>  *throughput summary: 13453.88 requests per second*  
>  *latency summary (msec):*  
>    *avg       min       p50       p95       p99    max*  
>    *3.627     0.864     3.263     5.583     7.463  1012.223*

While those were the results for the Redis node running on Docker:

> *Summary:*  
>  *throughput summary: 14967.15 requests per second*  
>  *latency summary (msec):*  
>    *avg       min       p50       p95       p99    max*  
>    *3.275     0.608     2.935     5.087     6.679  1006.591*

You can see that they are really close. More than that, we can multiply the number of the Skyhook nodes and significantly improve performance working with the same aerospike cluster. For instance, you can use the Round-robin DNS or any popular load balancer like HAProxy. This will not require any specific configuration since Skyhook is completely stateless.

## Summary

The project is constantly evolving, having more and more commands being supported. You can find the coverage of the supported Redis commands in the [repo readme](https://github.com/aerospike/skyhook).

It will be really great to hear from you about the experience of using it. Also, your contributions to the project are very welcome.

If you encounter a bug, please report it or [open an issue](https://github.com/aerospike/skyhook/issues) stating the commands you would like to be supported.
