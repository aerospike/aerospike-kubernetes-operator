# Aerospike Kubernetes Operator Log Collector

## Overview

script scrapper.sh collects all the required logs of kubernetes cluster, which are available with the cluster at the time of scripts being executed.


## What logs does it collect?

The script collects the following data:

* Describe output of all Pods, STS, PVC, AerospikeCluster in all namespaces.
* Logs of all containers of all Pod in all namespaces.
* Clusterwide events logs.


## How to run?

* Go to directory where user has the write permission.
* Make sure aerospike cluster is accessible and kubectl commands are working.
* Use absolute path of scrapper file <path>/scrapper.sh

## How does result looks like?

* Script will create a directory with name "scrapperlogs".
* Inside "scrapperlogs" directory, another directory will be created with name consist of kubernetes current config followed by current date.
* Inside that all cluster wide information will be available.
