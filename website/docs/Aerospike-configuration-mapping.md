---
title: Aerospike Configuration Mapping
description: Aerospike Configuration Mapping
---


## Mapping Between YAML and Aerospike Configuration

Kubernetes uses [YAML](https://YAML.org/) to express it's configuration whereas the Aerospike DB uses [it's own format for configuration](https://docs.aerospike.com/docs/configure/index.md) which it stores in `aerospike.conf`.

The Aerospike Kubernetes Operator translates it's YAML configurations to the Aerospike server's own `aerospike.conf` format.

Different Aerospike DB versions have may have different `aerospike.conf` representations. Please check [config-schemas](https://github.com/aerospike/aerospike-kubernetes-operator/tree/1.0.1/docs/config-schemas) for JSON schemas for all supported versions.

## Translation Conventions

These are the rules we use to translate between Kubernetes' YAML and Aerospike's `aerospike.conf` format.

### Simple Key and Values

Simple key value pairs file translate directly with the exception being [storage sizes](Aerospike-configuration-mapping.md#storage-sizes).

YAML

```yaml
replication-factor: 2
memory-size: 4294967296
```

`aerospike.conf`

```
replication-factor 2
memory-size 4G
```

### Storage Sizes

Memory, file, and devices sizes in the YAML format are integers and need to be specified as number of bytes. In `aerospike.conf` one may optionally provide a unit as a string.

YAML

```yaml
memory-size: 4294967296 # 4G
memory-size: 419430400  # 400M
```

`aerospike.conf`

```
memory-size 4G
memory-size 400M
```

### Lists of Simple Values

YAML uses a key's plural form when there are a list of values (and it uses a list type).

Lists of simple values are written in Aerospike by repeating the same configuration key multiple times.

YAML

```yaml
addresses:
  - 192.168.1.1
  - 192.168.5.1
```

`aerospike.conf`

```
address 192.168.1.1
address 192.168.5.1
```

YAML

```yaml
files:
  - /opt/aerospike/ns1.dat
  - /opt/aerospike/ns2.dat
```

`aerospike.conf`

```
file /opt/aerospike/ns1.dat
file /opt/aerospike/ns2.dat
```

### Fixed Sections

The Aerospike configuration has sections grouping parts of the configuration together. The YAML forms of these are represented as maps.

YAML

```yaml
service:
  service-threads: 4
  proto-fd-max: 15000
```

`aerospike.conf`

```
service {
  service-threads 4
  proto-fd-max 15000
}
```

### Named Sections

Named sections which can have multiple named entries in `aerospike.conf` file (eg `namespace`, `dc`, `datacenter`, etc.) will be translated to a named list of maps in YAML

The name of the list will be the plural form of the `aerospike.conf` section.

YAML

```yaml
namespaces:
  - name: test
    replication-factor: 2
    memory-size: 4294967296
    storage-engine:
      type: device
      files:
        - /opt/aerospike/data/test1.dat
        - /opt/aerospike/data/test2.dat
      filesize: 4294967296
      data-in-memory: true
  - name: bar
    replication-factor: 2
    memory-size: 4294967296
    storage-engine:
      type: memory
```

`aerospike.conf`

```
namespace test {
    replication-factor 2
    memory-size 4G
    storage-engine device {
            file /opt/aerospike/data/test1.dat
            file /opt/aerospike/data/test2.dat
            filesize 4G
            data-in-memory true
    }
}
namespace bar {
    replication-factor 2
    memory-size 4G
    storage-engine memory
}

```

### Typed Sections

Typed sections have a fixed enum type associated with them in `aerospike.conf` file (eg `storage-engine`, `index-type`, etc.) and will be translated to maps with additional property `type` in YAML.
The valid values for type will be the valid enum values for the section.

For e.g. storage-engine type property can have values memory, device, pmem.

YAML

```yaml
namespaces:
  - name: test
    .
    .
    storage-engine:
      type: device
      files:
        - /opt/aerospike/data/test1.dat
        - /opt/aerospike/data/test2.dat
      filesize: 4294967296
      data-in-memory: true
```

`aerospike.conf`

```
namespace test {
    .
    .
    storage-engine device {
            file /opt/aerospike/data/test1.dat
            file /opt/aerospike/data/test2.dat
            filesize 4G
            data-in-memory true
    }
}
```


## Complete Example

YAML

```yaml
service:
  proto-fd-max: 15000

security:
  enable-security: true

logging:
  - name: console
    any: info
  - name: /var/log/aerospike/aerospike.log
    any: info

xdr:
  enable-xdr: true
  xdr-digestlog-path: /opt/aerospike/xdr/digestlog 5G
  xdr-compression-threshold: 1000
  datacenters:
    - name: REMOTE_DC_1
      dc-node-address-ports:
       - 172.68.17.123 3000
      dc-security-config-file: /etc/aerospike/secret/security_credentials_DC1.txt

namespaces:
  - name: test
    enable-xdr: true
    xdr-remote-datacenter: REMOTE_DC_1
    replication-factor: 2
    memory-size: 4294967296
    storage-engine:
      type: device
      files:
        - /opt/aerospike/data/test.dat
      filesize: 4294967296
      data-in-memory: true # Store data in memory in addition to file.

mod-lua:
  user-path: /opt/aerospike/usr/udf/lua
```

`aerospike.conf`

```

service {                # Tuning parameters and process owner
    proto-fd-max 15000
}

security {               # (Optional, Enterprise Edition only) to enable
                         # ACL on the cluster
    enable-security true
}

logging {               # Logging configuration
    console {
        context any info
    }
    file /var/log/aerospike/aerospike.log {
        context any info
    }
}

xdr {
    enable-xdr true # Globally enable/disable XDR on local node.
    xdr-digestlog-path /opt/aerospike/digestlog 5G # Track digests to be shipped.
    xdr-compression-threshold 1000
    datacenter REMOTE_DC_1 {
            dc-node-address-port 172.68.17.123 3000
            dc-security-config-file /etc/aerospike/secret/security_credentials_DC1.txt
    }
}

namespace test {       # Define namespace record policies and storage engine
    enable-xdr true
    xdr-remote-datacenter REMOTE_DC_1
    replication-factor 2
    memory-size 4G
    storage-engine device {
            file /opt/aerospike/data/test.dat
            filesize 4G
            data-in-memory true # Store data in memory in addition to file.
    }
}

mod-lua {                # location of UDF modules
    user-path /opt/aerospike/usr/udf/lua
}
```
