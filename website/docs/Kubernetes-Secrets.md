---
title: Kubernetes Secrets
description: Kubernetes Secrets
---

Kubernetes secrets are used to store information securely. You can create secrets to setup Aerospike authentication, TLS, and features.conf. See [Manage-TLS-Certificates](Manage-TLS-Certificates.md) for more details.

## Create a Kubernetes secret for a folder

To create a Kubernetes secret for connectivity to the Aerospike cluster, the Aerospike features.conf can be packaged in a single directory and converted to Kubernetes secrets with the following command:

```sh
$ kubectl  -n aerospike create secret generic aerospike-secret --from-file=deploy/secrets
```

To deploy through the Operator, update the name of the secret in the aerospikeConfigSecret spec of the custom resource that defines the Aerospike cluster. You can also refer to files that are in the folder when you are configuring parameters for the Aerospike cluster in the aerospikeConfig spec of the custom resource. 


## Creating a Kubernetes secret for a user's password

To create a secret containing the password for Aerospike cluster admin user by passing the password from the command line:
```sh
$ kubectl  -n aerospike create secret generic auth-secret --from-literal=password='admin123'
```

To deploy with the Operator, you must include the names of the secrets for each user in the custom resource file. For example, suppose that you want to give two users access to the Aerospike cluster. For the first user, you create a secret named auth-secret. For the second user, you create a secret named user1-secret. To enable security for the cluster:

```sh
spec:
  .
  .
  enable-security: true
  .
  aerospikeAccessControl:
    users:
      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
      - name: user1
        secret-name: user1-secret
        roles:
          - data-admin
  .
  .
```

For guidelines to follow when creating passwords, refer to ["Local-to-Aerospike passwords"](https://docs.aerospike.com/docs/configure/security/access-control/index.md#local-to-aerospike-passwords).

