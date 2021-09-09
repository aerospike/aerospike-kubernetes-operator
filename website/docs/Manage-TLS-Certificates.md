---
title: Manage TLS Certificates
description: Manage TLS Certificates
---

Here we describe setting up a TLS enabled Aerospike cluster.

For more details, visit [TLS configuration](https://docs.aerospike.com/docs/configure/network/tls/).

## Create a secret containing TLS certificates and key.

Assuming your TLS secrets are in deploy/secrets folder, create a Kubernetes secret like so
```sh
$ kubectl create secret generic aerospike-secret --from-file=deploy/secrets -n aerospike
```

## Create the TLS specific Aerospike configuration.
TLS specific config for the Aerospike cluster CR file.

```yaml
  aerospikeConfigSecret:
    secretName: aerospike-secret
    mountPath:  /etc/aerospike/secret
  aerospikeConfig:
    network:
      service:
        tls-name: bob-cluster-a
        tls-authenticate-client: any
      heartbeat:
        tls-name: bob-cluster-b
      fabric:
        tls-name: bob-cluster-c
      tls:
        - name: bob-cluster-a
          cert-file: /etc/aerospike/secret/svc_cluster_chain.pem
          key-file: /etc/aerospike/secret/svc_key.pem
          ca-file: /etc/aerospike/secret/cacert.pem
        - name: bob-cluster-b
          cert-file: /etc/aerospike/secret/hb_cluster_chain.pem
          key-file: /etc/aerospike/secret/hb_key.pem
          ca-file: /etc/aerospike/secret/cacert.pem
        - name: bob-cluster-c
          cert-file: /etc/aerospike/secret/fb_cluster_chain.pem
          key-file: /etc/aerospike/secret/fb_key.pem
          ca-file: /etc/aerospike/secret/cacert.pem
```
Get full CR file [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/tls_cluster_cr.yaml).

## Deploy the cluster
Follow the instructions [here](Create-Aerospike-cluster.md#deploy-aerospike-cluster) to deploy this configuration.


