---
title: XDR
description: XDR
---

To deploy a cluster as XDR source, you should configure `dc-security-config-file` config in CR file in `aerospikeConfig.xdr.datacenter` section. Also configure `dc-node-address-port` in same section for destination DC. After configuring these values in the CR file  apply CR file to deploy the cluster.

For more details, visit [configure cross-datacenter](https://docs.aerospike.com/docs/configure/cross-datacenter/)

## Enable XDR and create a remote DC
Following is the XDR specific config for the Aerospike cluster CR file.
```yaml
  fileStorage:
    - storageClass: ssd
      volumeMounts:
        - mountPath: /opt/aerospike/data
          sizeInGB: 3
        - mountPath: /opt/aerospike/xdr
          sizeInGB: 5
  aerospikeConfigSecret:
    secretName: aerospike-secret
    mountPath:  /etc/aerospike/secret
  aerospikeConfig:
    xdr:
      enable-xdr: true
      xdr-digestlog-path: /opt/aerospike/xdr/digestlog 5G
      xdr-compression-threshold: 1000
      datacenters:
        - name: REMOTE_DC_1
          dc-node-address-port: "IP PORT"
          dc-security-config-file: /etc/aerospike/secret/security_credentials_DC1.txt
    namespaces:
      - name: test
        enable-xdr: true
        xdr-remote-datacenter: REMOTE_DC_1
        memory-size: 3000000000
        storage-engine:
          type: device
          files:
            - /opt/aerospike/data/test.dat
          filesize: 2000000000
          data-in-memory: true
```
Get full CR file [here](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/deploy/samples/xdr_src_cluster_cr.yaml).

## Remote DC Credentials
If destination cluster is security enabled then `aerospike-secret` created in this section should also have `security_credentials_DC1.txt` file for destination DC.

```sh
$ cat security_credentials_DC1.txt
credentials
{
   username xdr_user
   password xdr_pass
}
```

## Deploy the cluster
Follow the instructions [here](Create-Aerospike-cluster.md#deploy-aerospike-cluster) to deploy this configuration.
