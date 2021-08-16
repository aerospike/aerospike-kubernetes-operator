package configschema

const conf4_9_0 = `
{
  "$schema": "http://json-schema.org/draft-06/schema",
  "additionalProperties": false,
  "type": "object",
  "required": ["network", "namespaces"],
  "properties": {
    "service": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "user": {
          "type": "string",
          "default": "",
          "description": "",
          "dynamic": false
        },
        "group": {
          "type": "string",
          "default": "",
          "description": "",
          "dynamic": false
        },
        "paxos-single-replica-limit": {
          "type": "integer",
          "default": 1,
          "minimum": 0,
          "maximum": 128,
          "description": "",
          "dynamic": false
        },
        "pidfile": {
          "type": "string",
          "default": "",
          "description": "",
          "dynamic": false
        },
        "proto-fd-max": {
          "type": "integer",
          "default": 15000,
          "minimum": 0,
          "maximum": 2147483647,
          "description": "",
          "dynamic": true
        },
        "advertise-ipv6": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "auto-pin": {
          "type": "string",
          "description": "",
          "dynamic": false,
          "default": "none",
          "enum": ["none", "cpu", "numa", "adq"]
        },
        "batch-index-threads": {
          "type": "integer",
          "default": 1,
          "minimum": 1,
          "maximum": 256,
          "description": "",
          "dynamic": true
        },
        "batch-max-buffers-per-queue": {
          "type": "integer",
          "default": 255,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "batch-max-requests": {
          "type": "integer",
          "default": 5000,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "batch-max-unused-buffers": {
          "type": "integer",
          "default": 256,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "batch-without-digests": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "cluster-name": {
          "type": "string",
          "default": "",
          "description": "",
          "dynamic": true
        },
        "enable-benchmarks-fabric": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "enable-health-check": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "enable-hist-info": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "feature-key-file": {
          "type": "string",
          "default": "/opt/aerospike/data/features.conf",
          "description": "",
          "dynamic": false
        },
        "hist-track-back": {
          "type": "integer",
          "default": 300,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "hist-track-slice": {
          "type": "integer",
          "default": 10,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": false
        },
        "hist-track-thresholds": {
          "type": "string",
          "default": "",
          "description": "",
          "dynamic": true
        },
        "info-threads": {
          "type": "integer",
          "default": 16,
          "minimum": 0,
          "maximum": 2147483647,
          "description": "",
          "dynamic": false
        },
        "keep-caps-ssd-health": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        },
        "log-local-time": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        },
        "log-millis": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        },
        "migrate-fill-delay": {
          "type": "integer",
          "default": 0,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "migrate-max-num-incoming": {
          "type": "integer",
          "default": 4,
          "minimum": 0,
          "maximum": 256,
          "description": "",
          "dynamic": true
        },
        "migrate-threads": {
          "type": "integer",
          "default": 1,
          "minimum": 0,
          "maximum": 100,
          "description": "",
          "dynamic": true
        },
        "min-cluster-size": {
          "type": "integer",
          "default": 1,
          "minimum": 0,
          "maximum": 128,
          "description": "",
          "dynamic": true
        },
        "node-id": {
          "type": "string",
          "default": "BB989E07C0B51A0",
          "description": "",
          "dynamic": false
        },
        "node-id-interface": {
          "type": "string",
          "default": "",
          "description": "",
          "dynamic": false
        },
        "proto-fd-idle-ms": {
          "type": "integer",
          "default": 60000,
          "minimum": 0,
          "maximum": 2147483647,
          "description": "",
          "dynamic": true
        },
        "query-batch-size": {
          "type": "integer",
          "default": 100,
          "minimum": 0,
          "maximum": 2147483647,
          "description": "",
          "dynamic": true
        },
        "query-bufpool-size": {
          "type": "integer",
          "default": 256,
          "minimum": 1,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "query-in-transaction-thread": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "query-long-q-max-size": {
          "type": "integer",
          "default": 500,
          "minimum": 1,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "query-pre-reserve-partitions": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "query-priority": {
          "type": "integer",
          "default": 10,
          "minimum": 0,
          "maximum": 2147483647,
          "description": "",
          "dynamic": true
        },
        "query-priority-sleep-us": {
          "type": "integer",
          "default": 1,
          "minimum": 0,
          "maximum": 18446744073709552000,
          "description": "",
          "dynamic": true
        },
        "query-rec-count-bound": {
          "type": "integer",
          "default": 18446744073709552000,
          "minimum": 1,
          "maximum": 18446744073709552000,
          "description": "",
          "dynamic": true
        },
        "query-req-in-query-thread": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "query-req-max-inflight": {
          "type": "integer",
          "default": 100,
          "minimum": 1,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "query-short-q-max-size": {
          "type": "integer",
          "default": 500,
          "minimum": 1,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "query-threads": {
          "type": "integer",
          "default": 6,
          "minimum": 1,
          "maximum": 32,
          "description": "",
          "dynamic": true
        },
        "query-threshold": {
          "type": "integer",
          "default": 10,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "query-untracked-time-ms": {
          "type": "integer",
          "default": 1000,
          "minimum": 1,
          "maximum": 18446744073709552000,
          "description": "",
          "dynamic": true
        },
        "query-worker-threads": {
          "type": "integer",
          "default": 15,
          "minimum": 1,
          "maximum": 480,
          "description": "",
          "dynamic": true
        },
        "run-as-daemon": {
          "type": "boolean",
          "default": true,
          "description": "",
          "dynamic": false
        },
        "scan-max-done": {
          "type": "integer",
          "default": 100,
          "minimum": 0,
          "maximum": 1000,
          "description": "",
          "dynamic": true
        },
        "scan-threads-limit": {
          "type": "integer",
          "default": 128,
          "minimum": 1,
          "maximum": 1024,
          "description": "",
          "dynamic": true
        },
        "service-threads": {
          "type": "integer",
          "default": 1,
          "minimum": 1,
          "maximum": 4096,
          "description": "",
          "dynamic": false
        },
        "sindex-builder-threads": {
          "type": "integer",
          "default": 4,
          "minimum": 1,
          "maximum": 32,
          "description": "",
          "dynamic": true
        },
        "sindex-gc-max-rate": {
          "type": "integer",
          "default": 50000,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "sindex-gc-period": {
          "type": "integer",
          "default": 10,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "ticker-interval": {
          "type": "integer",
          "default": 10,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "transaction-max-ms": {
          "type": "integer",
          "default": 1000,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "transaction-retry-ms": {
          "type": "integer",
          "default": 1002,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "work-directory": {
          "type": "string",
          "default": "/opt/aerospike",
          "description": "",
          "dynamic": false
        },
        "debug-allocations": {
          "type": "string",
          "description": "",
          "dynamic": false,
          "default": "none",
          "enum": ["none", "transient", "persistent", "all"]
        },
        "indent-allocations": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        }
      }
    },
    "logging": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "name": {
            "type": "string",
            "default": " ",
            "description": "",
            "dynamic": false
          },
          "misc": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "alloc": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "arenax": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "hardware": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "msg": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "rbuffer": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "socket": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "tls": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "vmapx": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "xmem": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "aggr": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "appeal": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "as": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "batch": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "bin": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "config": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "clustering": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "drv_pmem": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "drv_ssd": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "exchange": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "fabric": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "flat": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "geo": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "hb": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "health": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "hlc": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "index": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "info": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "info-port": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "job": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "migrate": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "mon": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "namespace": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "nsup": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "particle": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "partition": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "paxos": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "predexp": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "proto": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "proxy": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "proxy-divert": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "query": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "record": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "roster": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "rw": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "rw-client": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "scan": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "security": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "service": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "service-list": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "sindex": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "skew": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "smd": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "storage": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "truncate": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "tsvc": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "udf": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "xdr": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "xdr-client": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "xdr-http": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          },
          "any": {
            "enum": ["CRITICAL", "critical", "WARNING", "warning", "INFO", "info", "DEBUG", "debug", "DETAIL", "detail"],
            "description": "",
            "dynamic": true,
            "default": "INFO"
          }
        }
      }
    },
    "network": {
      "type": "object",
      "additionalProperties": false,
      "required": ["service", "heartbeat", "fabric"],
      "properties": {
        "service": {
          "type": "object",
          "additionalProperties": false,
          "required": ["port"],
          "properties": {
            "addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            },
            "access-addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "access-port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            },
            "alternate-access-addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "alternate-access-port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            },
            "tls-access-addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "tls-access-port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            },
            "tls-addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "tls-alternate-access-addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "tls-alternate-access-port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            },
            "tls-authenticate-client": {
              "oneOf": [{
                "type": "string",
                "description": "",
                "dynamic": false,
                "default": "any",
                "enum": ["any", "false"]
              }, {
                "type": "array",
                "items": {
                  "type": "string",
					"format": "hostname"
                }
              }]
            },
            "tls-name": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "tls-port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            }
          }
        },
        "heartbeat": {
          "type": "object",
          "additionalProperties": false,
          "required": ["mode", "port"],
          "properties": {
            "mode": {
              "type": "string",
              "description": "",
              "dynamic": false,
              "default": "",
              "enum": ["mesh", "multicast"]
            },
            "addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "multicast-groups": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            },
            "mesh-seed-address-ports": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "interval": {
              "type": "integer",
              "default": 150,
              "minimum": 50,
              "maximum": 600000,
              "description": "",
              "dynamic": false
            },
            "timeout": {
              "type": "integer",
              "default": 10,
              "minimum": 3,
              "maximum": 4294967295,
              "description": "",
              "dynamic": false
            },
            "mtu": {
              "type": "integer",
              "default": 0,
              "minimum": 0,
              "maximum": 4294967295,
              "description": "",
              "dynamic": false
            },
            "multicast-ttl": {
              "type": "integer",
              "default": 0,
              "minimum": 0,
              "maximum": 255,
              "description": "",
              "dynamic": false
            },
            "protocol": {
              "type": "string",
              "description": "",
              "dynamic": false,
              "default": "v3",
              "enum": ["none", "v3"]
            },
            "tls-addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "tls-mesh-seed-address-ports": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "tls-name": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "tls-port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            }
          }
        },
        "fabric": {
          "type": "object",
          "additionalProperties": false,
          "required": ["port"],
          "properties": {
            "addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            },
            "channel-bulk-fds": {
              "type": "integer",
              "default": 2,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "channel-bulk-recv-threads": {
              "type": "integer",
              "default": 4,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "channel-ctrl-fds": {
              "type": "integer",
              "default": 1,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "channel-ctrl-recv-threads": {
              "type": "integer",
              "default": 4,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "channel-meta-fds": {
              "type": "integer",
              "default": 1,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "channel-meta-recv-threads": {
              "type": "integer",
              "default": 4,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "channel-rw-fds": {
              "type": "integer",
              "default": 8,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "channel-rw-recv-threads": {
              "type": "integer",
              "default": 16,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "keepalive-enabled": {
              "type": "boolean",
              "default": true,
              "description": "",
              "dynamic": false
            },
            "keepalive-intvl": {
              "type": "integer",
              "default": 1,
              "minimum": 1,
              "maximum": 2147483647,
              "description": "",
              "dynamic": false
            },
            "keepalive-probes": {
              "type": "integer",
              "default": 10,
              "minimum": 1,
              "maximum": 2147483647,
              "description": "",
              "dynamic": false
            },
            "keepalive-time": {
              "type": "integer",
              "default": 1,
              "minimum": 1,
              "maximum": 2147483647,
              "description": "",
              "dynamic": false
            },
            "latency-max-ms": {
              "type": "integer",
              "default": 5,
              "minimum": 0,
              "maximum": 1000,
              "description": "",
              "dynamic": false
            },
            "recv-rearm-threshold": {
              "type": "integer",
              "default": 1024,
              "minimum": 0,
              "maximum": 1048576,
              "description": "",
              "dynamic": false
            },
            "send-threads": {
              "type": "integer",
              "default": 8,
              "minimum": 1,
              "maximum": 128,
              "description": "",
              "dynamic": false
            },
            "tls-addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "tls-name": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "tls-port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            }
          }
        },
        "info": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "addresses": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "port": {
              "type": "integer",
              "default": 0,
              "minimum": 1024,
              "maximum": 65535,
              "description": "",
              "dynamic": false
            }
          }
        },
        "tls": {
          "type": "array",
          "items": {
            "type": "object",
            "additionalProperties": false,
            "properties": {
              "name": {
                "type": "string",
                "default": " ",
                "description": "",
                "dynamic": false
              },
              "ca-file": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": false
              },
              "ca-path": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": false
              },
              "cert-blacklist": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": false
              },
              "cert-file": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": false
              },
              "cipher-suite": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": false
              },
              "key-file": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": false
              },
              "key-file-password": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": false
              },
              "protocols": {
                "type": "string",
                "default": "TLSv1.2",
                "description": "",
                "dynamic": false
              }
            }
          }
        }
      }
    },
    "namespaces": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["memory-size"],
        "properties": {
          "name": {
            "type": "string",
            "default": " ",
            "description": "",
            "dynamic": false
          },
          "replication-factor": {
            "type": "integer",
            "default": 2,
            "minimum": 1,
            "maximum": 128,
            "description": "",
            "dynamic": false
          },
          "memory-size": {
            "type": "integer",
            "default": 0,
            "minimum": 0,
            "maximum": 18446744073709552000,
            "description": "",
            "dynamic": true
          },
          "default-ttl": {
            "type": "integer",
            "default": 0,
            "minimum": 0,
            "maximum": 18446744073709552000,
            "description": "",
            "dynamic": true
          },
          "storage-engine": {
            "oneOf": [{
              "type": "object",
              "additionalProperties": false,
              "required": ["type"],
              "properties": {
                "type": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "memory",
                  "enum": ["memory"]
                }
              }
            }, {
              "type": "object",
              "additionalProperties": false,
              "oneOf": [{
                "required": ["type", "devices"]
              }, {
                "required": ["type", "files"]
              }],
              "properties": {
                "type": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "device",
                  "enum": ["device"]
                },
                "devices": {
                  "type": "array",
                  "items": {
                    "type": "string"
                  },
                  "description": "",
                  "dynamic": false,
                  "default": []
                },
                "files": {
                  "type": "array",
                  "items": {
                    "type": "string"
                  },
                  "description": "",
                  "dynamic": false,
                  "default": []
                },
                "filesize": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 1048576,
                  "maximum": 2199023255552,
                  "description": "",
                  "dynamic": false
                },
                "scheduler-mode": {
                  "type": "string",
                  "default": "",
                  "enum": ["anticipatory", "cfq", "deadline", "noop", "null"],
                  "description": "",
                  "dynamic": false
                },
                "write-block-size": {
                  "type": "integer",
                  "default": 1048576,
                  "minimum": 1024,
                  "maximum": 8388608,
                  "description": "",
                  "dynamic": false
                },
                "data-in-memory": {
                  "type": "boolean",
                  "default": true,
                  "description": "",
                  "dynamic": false
                },
                "cache-replica-writes": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": true
                },
                "cold-start-empty": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "commit-to-device": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "commit-min-size": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 8388608,
                  "description": "",
                  "dynamic": false
                },
                "compression": {
                  "type": "string",
                  "description": "",
                  "dynamic": true,
                  "default": "none",
                  "enum": ["none", "lz4", "snappy", "zstd"]
                },
                "compression-level": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 9,
                  "description": "",
                  "dynamic": true
                },
                "defrag-lwm-pct": {
                  "type": "integer",
                  "default": 50,
                  "minimum": 1,
                  "maximum": 99,
                  "description": "",
                  "dynamic": true
                },
                "defrag-queue-min": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 4294967295,
                  "description": "",
                  "dynamic": true
                },
                "defrag-sleep": {
                  "type": "integer",
                  "default": 1000,
                  "minimum": 0,
                  "maximum": 4294967295,
                  "description": "",
                  "dynamic": true
                },
                "defrag-startup-minimum": {
                  "type": "integer",
                  "default": 10,
                  "minimum": 1,
                  "maximum": 99,
                  "description": "",
                  "dynamic": false
                },
                "direct-files": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "disable-odsync": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "enable-benchmarks-storage": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": true
                },
                "encryption": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "aes-128",
                  "enum": ["aes-128", "aes-256"]
                },
                "encryption-key-file": {
                  "type": "string",
                  "default": "",
                  "description": "",
                  "dynamic": false
                },
                "flush-max-ms": {
                  "type": "integer",
                  "default": 1000,
                  "minimum": 0,
                  "maximum": 1000,
                  "description": "",
                  "dynamic": true
                },
                "max-write-cache": {
                  "type": "integer",
                  "default": 67108864,
                  "minimum": 0,
                  "maximum": 18446744073709552000,
                  "description": "",
                  "dynamic": true
                },
                "min-avail-pct": {
                  "type": "integer",
                  "default": 5,
                  "minimum": 0,
                  "maximum": 100,
                  "description": "",
                  "dynamic": true
                },
                "post-write-queue": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 4096,
                  "description": "",
                  "dynamic": true
                },
                "read-page-cache": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": true
                },
                "serialize-tomb-raider": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "tomb-raider-sleep": {
                  "type": "integer",
                  "default": 1000,
                  "minimum": 0,
                  "maximum": 4294967295,
                  "description": "",
                  "dynamic": true
                }
              }
            }, {
              "type": "object",
              "additionalProperties": false,
              "required": ["type", "files"],
              "properties": {
                "type": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "pmem",
                  "enum": ["pmem"]
                },
                "files": {
                  "type": "array",
                  "items": {
                    "type": "string"
                  },
                  "description": "",
                  "dynamic": false,
                  "default": []
                },
                "filesize": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 1048576,
                  "maximum": 2199023255552,
                  "description": "",
                  "dynamic": false
                },
                "commit-to-device": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "compression": {
                  "type": "string",
                  "description": "",
                  "dynamic": true,
                  "default": "none",
                  "enum": ["none", "lz4", "snappy", "zstd"]
                },
                "compression-level": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 9,
                  "description": "",
                  "dynamic": true
                },
                "defrag-lwm-pct": {
                  "type": "integer",
                  "default": 50,
                  "minimum": 1,
                  "maximum": 99,
                  "description": "",
                  "dynamic": true
                },
                "defrag-queue-min": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 4294967295,
                  "description": "",
                  "dynamic": true
                },
                "defrag-sleep": {
                  "type": "integer",
                  "default": 1000,
                  "minimum": 0,
                  "maximum": 4294967295,
                  "description": "",
                  "dynamic": true
                },
                "defrag-startup-minimum": {
                  "type": "integer",
                  "default": 10,
                  "minimum": 1,
                  "maximum": 99,
                  "description": "",
                  "dynamic": false
                },
                "direct-files": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "disable-odsync": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "enable-benchmarks-storage": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": true
                },
                "encryption": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "aes-128",
                  "enum": ["aes-128", "aes-256"]
                },
                "encryption-key-file": {
                  "type": "string",
                  "default": "",
                  "description": "",
                  "dynamic": false
                },
                "flush-max-ms": {
                  "type": "integer",
                  "default": 1000,
                  "minimum": 0,
                  "maximum": 1000,
                  "description": "",
                  "dynamic": true
                },
                "max-write-cache": {
                  "type": "integer",
                  "default": 67108864,
                  "minimum": 0,
                  "maximum": 18446744073709552000,
                  "description": "",
                  "dynamic": true
                },
                "min-avail-pct": {
                  "type": "integer",
                  "default": 5,
                  "minimum": 0,
                  "maximum": 100,
                  "description": "",
                  "dynamic": true
                },
                "serialize-tomb-raider": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "tomb-raider-sleep": {
                  "type": "integer",
                  "default": 1000,
                  "minimum": 0,
                  "maximum": 4294967295,
                  "description": "",
                  "dynamic": true
                }
              }
            }]
          },
          "enable-xdr": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "sets-enable-xdr": {
            "type": "boolean",
            "default": true,
            "description": "",
            "dynamic": true
          },
          "xdr-remote-datacenters": {
            "type": "array",
            "items": {
              "type": "string"
            },
            "description": "",
            "dynamic": true,
            "default": []
          },
          "ns-forward-xdr-writes": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "allow-nonxdr-writes": {
            "type": "boolean",
            "default": true,
            "description": "",
            "dynamic": true
          },
          "allow-xdr-writes": {
            "type": "boolean",
            "default": true,
            "description": "",
            "dynamic": true
          },
          "allow-ttl-without-nsup": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "background-scan-max-rps": {
            "type": "integer",
            "default": 10000,
            "minimum": 1,
            "maximum": 1000000,
            "description": "",
            "dynamic": true
          },
          "conflict-resolution-policy": {
            "type": "string",
            "description": "",
            "dynamic": false,
            "default": "generation",
            "enum": ["generation", "last-update-time"]
          },
          "data-in-index": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": false
          },
          "disable-cold-start-eviction": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": false
          },
          "disable-write-dup-res": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "disallow-null-setname": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "enable-benchmarks-batch-sub": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "enable-benchmarks-ops-sub": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "enable-benchmarks-read": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "enable-benchmarks-udf": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "enable-benchmarks-udf-sub": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "enable-benchmarks-write": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "enable-hist-proxy": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "evict-hist-buckets": {
            "type": "integer",
            "default": 10000,
            "minimum": 100,
            "maximum": 10000000,
            "description": "",
            "dynamic": true
          },
          "evict-tenths-pct": {
            "type": "integer",
            "default": 5,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "high-water-disk-pct": {
            "type": "integer",
            "default": 0,
            "minimum": 0,
            "maximum": 100,
            "description": "",
            "dynamic": true
          },
          "high-water-memory-pct": {
            "type": "integer",
            "default": 0,
            "minimum": 0,
            "maximum": 100,
            "description": "",
            "dynamic": true
          },
          "index-stage-size": {
            "type": "integer",
            "default": 1073741824,
            "minimum": 134217728,
            "maximum": 17179869184,
            "description": "",
            "dynamic": false
          },
          "index-type": {
            "oneOf": [{
              "type": "object",
              "additionalProperties": false,
              "required": ["type"],
              "properties": {
                "type": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "shmem",
                  "enum": ["shmem"]
                }
              }
            }, {
              "type": "object",
              "additionalProperties": false,
              "required": ["type", "mounts", "mounts-size-limit"],
              "properties": {
                "type": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "pmem",
                  "enum": ["pmem"]
                },
                "mounts": {
                  "type": "array",
                  "items": {
                    "type": "string"
                  },
                  "description": "",
                  "dynamic": false,
                  "default": []
                },
                "mounts-high-water-pct": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 100,
                  "description": "",
                  "dynamic": true
                },
                "mounts-size-limit": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 1073741824,
                  "maximum": 18446744073709551615,
                  "description": "",
                  "dynamic": true
                }
              }
            }, {
              "type": "object",
              "additionalProperties": false,
              "required": ["type", "mounts", "mounts-size-limit"],
              "properties": {
                "type": {
                  "type": "string",
                  "description": "",
                  "dynamic": false,
                  "default": "flash",
                  "enum": ["flash"]
                },
                "mounts": {
                  "type": "array",
                  "items": {
                    "type": "string"
                  },
                  "description": "",
                  "dynamic": false,
                  "default": []
                },
                "mounts-high-water-pct": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 100,
                  "description": "",
                  "dynamic": true
                },
                "mounts-size-limit": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 4294967296,
                  "maximum": 18446744073709551615,
                  "description": "",
                  "dynamic": true
                }
              }
            }]
          },
          "migrate-order": {
            "type": "integer",
            "default": 5,
            "minimum": 1,
            "maximum": 10,
            "description": "",
            "dynamic": true
          },
          "migrate-retransmit-ms": {
            "type": "integer",
            "default": 5000,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "migrate-sleep": {
            "type": "integer",
            "default": 1,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "nsup-hist-period": {
            "type": "integer",
            "default": 3600,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "nsup-period": {
            "type": "integer",
            "default": 120,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "nsup-threads": {
            "type": "integer",
            "default": 1,
            "minimum": 1,
            "maximum": 128,
            "description": "",
            "dynamic": true
          },
          "partition-tree-sprigs": {
            "type": "integer",
            "default": 256,
            "minimum": 16,
            "maximum": 4096,
            "description": "",
            "dynamic": false
          },
          "prefer-uniform-balance": {
            "type": "boolean",
            "default": true,
            "description": "",
            "dynamic": true
          },
          "rack-id": {
            "type": "integer",
            "default": 0,
            "minimum": 0,
            "maximum": 1000000,
            "description": "",
            "dynamic": true
          },
          "read-consistency-level-override": {
            "type": "string",
            "description": "",
            "dynamic": false,
            "default": "off",
            "enum": ["all", "off", "one"]
          },
          "sets": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "properties": {
                "name": {
                  "type": "string",
                  "default": " ",
                  "description": "",
                  "dynamic": false
                },
                "set-disable-eviction": {
                  "type": "boolean",
                  "default": false,
                  "description": "",
                  "dynamic": false
                },
                "set-enable-xdr": {
                  "type": "string",
                  "default": "use-default",
                  "description": "",
                  "dynamic": false
                },
                "set-stop-writes-count": {
                  "type": "integer",
                  "default": 0,
                  "minimum": 0,
                  "maximum": 18446744073709552000,
                  "description": "",
                  "dynamic": true
                }
              }
            }
          },
          "sindex": {
            "type": "object",
            "additionalProperties": false,
            "properties": {
              "num-partitions": {
                "type": "integer",
                "default": 32,
                "minimum": 1,
                "maximum": 256,
                "description": "",
                "dynamic": false
              }
            }
          },
          "geo2dsphere-within": {
            "type": "object",
            "additionalProperties": false,
            "properties": {
              "strict": {
                "type": "boolean",
                "default": true,
                "description": "",
                "dynamic": false
              },
              "min-level": {
                "type": "integer",
                "default": 1,
                "minimum": 0,
                "maximum": 30,
                "description": "",
                "dynamic": false
              },
              "max-level": {
                "type": "integer",
                "default": 30,
                "minimum": 0,
                "maximum": 30,
                "description": "",
                "dynamic": false
              },
              "max-cells": {
                "type": "integer",
                "default": 12,
                "minimum": 1,
                "maximum": 256,
                "description": "",
                "dynamic": true
              },
              "level-mod": {
                "type": "integer",
                "default": 1,
                "minimum": 1,
                "maximum": 3,
                "description": "",
                "dynamic": false
              },
              "earth-radius-meters": {
                "type": "integer",
                "default": 6371000,
                "minimum": 0,
                "maximum": 4294967295,
                "description": "",
                "dynamic": false
              }
            }
          },
          "single-bin": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": false
          },
          "single-scan-threads": {
            "type": "integer",
            "default": 4,
            "minimum": 1,
            "maximum": 128,
            "description": "",
            "dynamic": true
          },
          "stop-writes-pct": {
            "type": "integer",
            "default": 90,
            "minimum": 0,
            "maximum": 100,
            "description": "",
            "dynamic": true
          },
          "strong-consistency": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": false
          },
          "strong-consistency-allow-expunge": {
            "type": "boolean",
            "default": false,
            "description": "",
            "dynamic": true
          },
          "tomb-raider-eligible-age": {
            "type": "integer",
            "default": 86400,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "tomb-raider-period": {
            "type": "integer",
            "default": 86400,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "transaction-pending-limit": {
            "type": "integer",
            "default": 20,
            "minimum": 0,
            "maximum": 4294967295,
            "description": "",
            "dynamic": true
          },
          "truncate-threads": {
            "type": "integer",
            "default": 4,
            "minimum": 1,
            "maximum": 128,
            "description": "",
            "dynamic": true
          },
          "write-commit-level-override": {
            "type": "string",
            "description": "",
            "dynamic": false,
            "default": "off",
            "enum": ["all", "master", "off"]
          }
        }
      }
    },
    "mod-lua": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "cache-enabled": {
          "type": "boolean",
          "default": true,
          "description": "",
          "dynamic": false
        },
        "user-path": {
          "type": "string",
          "default": "/opt/aerospike/usr/udf/lua",
          "description": "",
          "dynamic": false
        }
      }
    },
    "security": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "enable-ldap": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        },
        "enable-security": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        },
        "ldap-login-threads": {
          "type": "integer",
          "default": 8,
          "minimum": 1,
          "maximum": 64,
          "description": "",
          "dynamic": true
        },
        "privilege-refresh-period": {
          "type": "integer",
          "default": 300,
          "minimum": 10,
          "maximum": 86400,
          "description": "",
          "dynamic": true
        },
        "ldap": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "disable-tls": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "polling-period": {
              "type": "integer",
              "default": 300,
              "minimum": 0,
              "maximum": 86400,
              "description": "",
              "dynamic": true
            },
            "query-base-dn": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "query-user-dn": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "query-user-password-file": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "role-query-base-dn": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "role-query-patterns": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": false,
              "default": []
            },
            "role-query-search-ou": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "server": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "session-ttl": {
              "type": "integer",
              "default": 86400,
              "minimum": 120,
              "maximum": 864000,
              "description": "",
              "dynamic": true
            },
            "tls-ca-file": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "token-hash-method": {
              "type": "string",
              "default": "sha-256",
              "description": "",
              "dynamic": false
            },
            "user-dn-pattern": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            },
            "user-query-pattern": {
              "type": "string",
              "default": "",
              "description": "",
              "dynamic": false
            }
          }
        },
        "log": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "report-authentication": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "report-data-op": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": true,
              "default": []
            },
            "report-sys-admin": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "report-user-admin": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "report-violation": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            }
          }
        },
        "syslog": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "local": {
              "type": "integer",
              "default": -1,
              "minimum": 0,
              "maximum": 7,
              "description": "",
              "dynamic": false
            },
            "report-authentication": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "report-data-op": {
              "type": "array",
              "items": {
                "type": "string"
              },
              "description": "",
              "dynamic": true,
              "default": []
            },
            "report-sys-admin": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "report-user-admin": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            },
            "report-violation": {
              "type": "boolean",
              "default": false,
              "description": "",
              "dynamic": false
            }
          }
        }
      }
    },
    "xdr": {
      "type": "object",
      "additionalProperties": false,
      "required": ["xdr-digestlog-path"],
      "properties": {
        "enable-xdr": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "enable-change-notification": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        },
        "xdr-digestlog-path": {
          "type": "string",
          "default": "null 0",
          "description": "",
          "dynamic": false
        },
        "datacenters": {
          "type": "array",
          "minItems": 1,
          "items": {
            "type": "object",
            "additionalProperties": false,
            "properties": {
              "name": {
                "type": "string",
                "default": " ",
                "description": "",
                "dynamic": false
              },
              "auth-mode": {
                "type": "string",
                "description": "",
                "dynamic": false,
                "default": "internal",
                "enum": ["internal", "external", "external-insecure"]
              },
              "dc-connections": {
                "type": "integer",
                "default": 64,
                "minimum": 0,
                "maximum": 4294967295,
                "description": "",
                "dynamic": true
              },
              "dc-connections-idle-ms": {
                "type": "integer",
                "default": 55000,
                "minimum": 0,
                "maximum": 4294967295,
                "description": "",
                "dynamic": true
              },
              "dc-int-ext-ipmap": {
                "type": "array",
                "items": {
                  "type": "string"
                },
                "description": "",
                "dynamic": true,
                "default": []
              },
              "dc-node-address-ports": {
                "type": "array",
                "items": {
                  "type": "string"
                },
                "description": "",
                "dynamic": false,
                "default": []
              },
              "dc-security-config-file": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": true
              },
              "dc-ship-bins": {
                "type": "boolean",
                "default": true,
                "description": "",
                "dynamic": true
              },
              "dc-type": {
                "type": "string",
                "default": "aerospike",
                "enum": ["aerospike", "http", "null"],
                "description": "",
                "dynamic": true
              },
              "dc-use-alternate-services": {
                "type": "boolean",
                "default": false,
                "description": "",
                "dynamic": true
              },
              "http-urls": {
                "type": "array",
                "items": {
                  "type": "string"
                },
                "description": "",
                "dynamic": false,
                "default": []
              },
              "http-version": {
                "type": "string",
                "default": "v1",
                "enum": ["v1", "null"],
                "description": "",
                "dynamic": true
              },
              "tls-name": {
                "type": "string",
                "default": "",
                "description": "",
                "dynamic": true
              },
              "tls-nodes": {
                "type": "array",
                "items": {
                  "type": "string"
                },
                "description": "",
                "dynamic": false,
                "default": []
              },
              "dc-name": {
                "type": "string",
                "default": " ",
                "description": "",
                "dynamic": false
              }
            }
          }
        },
        "xdr-client-threads": {
          "type": "integer",
          "default": 3,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": false
        },
        "xdr-compression-threshold": {
          "type": "integer",
          "default": 0,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "xdr-delete-shipping-enabled": {
          "type": "boolean",
          "default": true,
          "description": "",
          "dynamic": false
        },
        "xdr-digestlog-iowait-ms": {
          "type": "integer",
          "default": 500,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "forward-xdr-writes": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "xdr-hotkey-time-ms": {
          "type": "integer",
          "default": 100,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "xdr-info-port": {
          "type": "integer",
          "default": 0,
          "minimum": 1024,
          "maximum": 65535,
          "description": "",
          "dynamic": false
        },
        "xdr-info-timeout": {
          "type": "integer",
          "default": 10000,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "xdr-max-ship-bandwidth": {
          "type": "integer",
          "default": 0,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "xdr-max-ship-throughput": {
          "type": "integer",
          "default": 0,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "xdr-min-digestlog-free-pct": {
          "type": "integer",
          "default": 0,
          "minimum": 0,
          "maximum": 100,
          "description": "",
          "dynamic": true
        },
        "xdr-nsup-deletes-enabled": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": true
        },
        "xdr-read-threads": {
          "type": "integer",
          "default": 4,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "xdr-ship-bins": {
          "type": "boolean",
          "default": false,
          "description": "",
          "dynamic": false
        },
        "xdr-ship-delay": {
          "type": "integer",
          "default": 0,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        },
        "xdr-shipping-enabled": {
          "type": "boolean",
          "default": true,
          "description": "",
          "dynamic": true
        },
        "xdr-write-timeout": {
          "type": "integer",
          "default": 10000,
          "minimum": 0,
          "maximum": 4294967295,
          "description": "",
          "dynamic": true
        }
      }
    }
  }
}`
