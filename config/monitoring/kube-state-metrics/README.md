# AerospikeCluster condition metrics

AKO 4.6.0+ writes status conditions on every `AerospikeCluster`: `Ready`, `Paused`, and one condition per
operation (`ScalingUp`, `ScalingDown`, `Upgrading`, `RollingRestart`, `RackRevisionRollingOut`). All seven are
present from the first reconcile. The files in this directory make the Datadog Cluster Agent's
`kubernetes_state_core` check (kube-state-metrics custom-resource-state) export them as metrics, so you can
alert on an operation that has been running too long. No AKO code or configuration change is needed.

This directory is not part of `config/monitoring/kustomization.yaml`; install it separately.

| File | Use |
|---|---|
| `datadogagent.yaml` | `DatadogAgent` fields for clusters where the Datadog Operator deploys the agents. Merge `spec.features.kubeStateMetricsCore` into your `DatadogAgent`. |
| `clusterrole.yaml` | `list`/`watch` on `aerospikeclusters`, bound to the **Datadog Operator's** service account. Required with the Operator unless it is installed with `clusterRole.allowReadAllResources=true`. |
| `datadog-values.yaml` | Values for the Datadog Helm chart. The chart creates the RBAC itself; `clusterrole.yaml` is not needed. |

`datadogagent.yaml` and `datadog-values.yaml` carry the same `collectCrMetrics` entry. `collectCrMetrics` is a
list: if you already collect other custom resources, add this entry to your list instead of replacing it.

## Requirements

- AKO 4.6.0 or later. Earlier versions do not write conditions, so no series are produced.
- Datadog Cluster Agent 7.63.0 or later.

## Install with the Datadog Operator

```sh
kubectl apply -f clusterrole.yaml
kubectl create clusterrolebinding aerospikecluster-metrics-reader \
  --clusterrole=aerospikecluster-metrics-reader \
  --serviceaccount=<operator-namespace>:<operator-service-account>
```

Then add `spec.features.kubeStateMetricsCore` from `datadogagent.yaml` to your `DatadogAgent` and apply it.

If the binding is missing, the Operator cannot create the Cluster Agent's role for `aerospikeclusters`. The
`DatadogAgent` status then shows `DatadogAgentInternalReconcileError` with
`is attempting to grant RBAC permissions not currently held`, and the Cluster Agent is not deployed.

## Install with the Datadog Helm chart

```sh
helm upgrade <release> datadog/datadog -f <your-values>.yaml -f datadog-values.yaml
```

## Metrics

| Metric in Datadog | Value |
|---|---|
| `kubernetes_state_customresource.aerospikecluster_status_condition` | One series per `status` (`True`, `False`, `Unknown`): `1` for the condition's current status, `0` for the other two |
| `kubernetes_state_customresource.aerospikecluster_status_condition_last_transition_time` | Unix time of the condition's last status change |

For 7 conditions that is 21 `status_condition` series and 7 `status_condition_last_transition_time` series per
`AerospikeCluster`.

Tags on every series:

| Tag | Source |
|---|---|
| `cluster_name` | `AerospikeCluster` name |
| `cluster_namespace` | `AerospikeCluster` namespace |
| `type` | Condition type, for example `RollingRestart` |
| `status` | `True`, `False` or `Unknown`; only on `status_condition` |
| `customresource_group`, `customresource_kind`, `customresource_version` | Added by kube-state-metrics |
| `kube_cluster_name` | Kubernetes cluster name, added by the Cluster Agent when `clusterName` is set |

Two `AerospikeCluster`s with the same name and namespace in different Kubernetes clusters are told apart by
`kube_cluster_name`, so include it in monitor groupings.

## Example monitors

An operation condition is `True` only while AKO is running that operation. "Stuck" means it has been `True` for
the whole window, so use `min` over the window. Datadog stores tag values in lower case.

Rolling restart running for more than 30 minutes, excluding paused clusters:

```
min(last_30m):min:kubernetes_state_customresource.aerospikecluster_status_condition{type:rollingrestart,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} - min:kubernetes_state_customresource.aerospikecluster_status_condition{type:paused,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} >= 1
```

Scale-down running for more than 60 minutes, excluding paused clusters:

```
min(last_60m):min:kubernetes_state_customresource.aerospikecluster_status_condition{type:scalingdown,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} - min:kubernetes_state_customresource.aerospikecluster_status_condition{type:paused,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} >= 1
```

Upgrade running for more than 60 minutes, excluding paused clusters:

```
min(last_60m):min:kubernetes_state_customresource.aerospikecluster_status_condition{type:upgrading,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} - min:kubernetes_state_customresource.aerospikecluster_status_condition{type:paused,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} >= 1
```

The windows are starting points. Each condition covers the whole operation, not one batch, so the time it stays
`True` grows with the number of pods, `rollingUpdateBatchSize` / `scaleDownBatchSize`, and how long migrations
take between batches. A scale-down waits for data to migrate off every removed pod, and an upgrade recreates
every pod, so both get a longer window than a rolling restart here. Tune each window to the longest healthy
operation you see on your clusters. `scalingup` and `rackrevisionrollingout` use the same form.

`Ready` not `True` (`False` or `Unknown`) for more than 30 minutes, excluding paused clusters:

```
max(last_30m):max:kubernetes_state_customresource.aerospikecluster_status_condition{type:ready,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} + max:kubernetes_state_customresource.aerospikecluster_status_condition{type:paused,status:true} by {kube_cluster_name,cluster_namespace,cluster_name} < 1
```

- **Paused clusters.** While `spec.paused` is true AKO does not reconcile, and an operation condition that was
  `True` stays `True` with its original `lastTransitionTime`. Without the `paused` term the monitor fires on a
  cluster that was paused on purpose.
- **Long migrations are healthy.** A rolling restart waits for migrations between batches, so `RollingRestart`
  can stay `True` for a long time on a large cluster. Pair the monitor with an Aerospike migration-progress
  metric, for example `aerospike_namespace_migrate_rx_partitions_remaining` not decreasing.
- **No data.** Deleting an `AerospikeCluster` removes its series. Set the monitor to not notify on missing data.
- **`Ready=True` duration** is the time since the last successful reconcile, not since the last operation.
  Every reconcile briefly sets `Ready` to `False`, including the first one after an operator restart.

## Time in the current state

For a dashboard, `time()` returns the current Unix time, so the seconds since a condition last changed are:

```
time() - max:kubernetes_state_customresource.aerospikecluster_status_condition_last_transition_time{type:rollingrestart} by {kube_cluster_name,cluster_namespace,cluster_name}
```

This is how long the rolling restart has been running while `status_condition{type:rollingrestart,status:true}` is `1`, and
how long since it finished while it is `0`.
