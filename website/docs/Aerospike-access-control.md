---
title: Aerospike Access Control
description: Aerospike Access Control
---


Aerospike Access Control includes user, role, and privilege creation and maintenance. For more details see [here](https://docs.aerospike.com/docs/configure/security/access-control/).

To manage your access controls from the operator, configure the `aerospikeAccessControl` section in the Aerospike cluster's Custom Resource (CR) file.

Here are a few examples for common access control tasks:

:::note
For these examples, assume that cluster is deployed using a file named `aerospike-cluster.yaml`.
:::

- [Creating a role](Aerospike-access-control.md#creating-a-role)
- [Adding privileges to a role](Aerospike-access-control.md#adding-privileges-to-a-role)
- [Removing privileges from a role](Aerospike-access-control.md#removing-privileges-from-a-role)
- [Creating a user with roles](Aerospike-access-control.md#creating-a-user-with-roles)
- [Add roles to a user](Aerospike-access-control.md#add-new-roles-to-a-user)
- [Removing roles from a user](Aerospike-access-control.md#removing-roles-from-a-user)
- [Changing a user's password](Aerospike-access-control.md#changing-a-user-s-password)
- [Dropping a role](Aerospike-access-control.md#dropping-a-role)
- [Dropping a user](Aerospike-access-control.md#dropping-a-user)

## Creating a role

Add a role in `roles` list under `aerospikeAccessControl`.

`sys-admin` and `user-admin` are standard predefined roles. Here we are adding a new custom role called "profiler" which is given `read` privileges.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    roles: 
      - name: profiler
        privileges: 
          - read
    users:
      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

To apply the change, run this command:

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Adding privileges to a role

Add the `read` and `read-write` privileges to the `profiler` role in `roles` list under `aerospikeAccessControl`.

```yaml

apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    roles: 
      - name: profiler
        privileges: 
          - read
          - read-write
    users:
      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

To apply the change, run this command

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Removing privileges from a role

Remove privileges from the desired role in `roles` list under `aerospikeAccessControl`.

Remove `read-write` `privilege`.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    roles: 
      - name: profiler
        privileges: 
          - read
    users:
      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

Apply the change by running `apply` with the updated config.

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Creating a user with roles

Create the secret for the user and add the user in `users` list under `aerospikeAccessControl`.

Create a secret `profile-user-secret` containing the password for the user `profiler` by passing the password from the command line:

```sh
kubectl  -n aerospike create secret generic profile-user-secret --from-literal=password='userpass'
```

Add `profileUser` user having `profiler` role.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    roles: 
      - name: profiler
        privileges: 
          - read
    users:
      - name: profileUser
        secretName: profile-user-secret
        roles:
          - profiler

      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

Apply the change:

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Add new roles to a user

Add roles in the desired user's `roles` list.

Add `user-admin`, `sys-admin` in `profileUser` roles list.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    roles: 
      - name: profiler
        privileges: 
          - read
    users:
      - name: profileUser
        secretName: profile-user-secret
        roles:
          - profiler
          - user-admin
          - sys-admin

      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

Apply the change:

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Removing roles from a user

Remove roles from the desired user's `roles` list.

Remove `sys-admin` from the `profileUser's` roles list.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    roles: 
      - name: profiler
        privileges: 
          - read
    users:
      - name: profileUser
        secretName: profile-user-secret
        roles:
          - profiler
          - user-admin

      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

Apply the change:

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Changing a user's password

Create a new secret `new-profile-user-secret` containing the password for Aerospike cluster user `profileUser` by passing the password from the command line:

```sh
kubectl  -n aerospike create secret generic new-profile-user-secret --from-literal=password='newuserpass'
```

Update the `secretName` for `profileUser` to the new secret name `new-profile-user-secret`.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    roles: 
      - name: profiler
        privileges: 
          - read
    users:
      - name: profileUser
        secretName: new-profile-user-secret
        roles:
          - profiler
          - user-admin

      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

Apply the change:

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Dropping a role

Remove the desired role from `roles` list under `aerospikeAccessControl`. Also remove this role from the `roles` list of all the users.

Remove `profiler` role.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
  .
  .
  aerospikeAccessControl:
    users:
      - name: profileUser
        secretName: new-profile-user-secret
        roles:
          - sys-admin

      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

Apply the change:

```sh
kubectl apply -f aerospike-cluster.yaml
```

## Dropping a user

Remove the desired user from the `users` list under `aerospikeAccessControl`.

Remove `profileUser` user.

```yaml
apiVersion: aerospike.com/v1alpha1
kind: AerospikeCluster
metadata:
  name: aerocluster
  namespace: aerospike

spec:
.
.
  aerospikeAccessControl:
    users:
      - name: admin
        secretName: auth-secret
        roles:
          - sys-admin
          - user-admin
```

Apply the change:

```sh
kubectl apply -f aerospike-cluster.yaml
```
