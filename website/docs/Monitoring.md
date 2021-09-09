---
title: Monitoring
description: Monitoring
---

[Aerospike Monitoring Stack](https://docs.aerospike.com/docs/tools/monitorstack/index.md) can be used to enable monitoring and alerting for Aerospike clusters deployed by the Aerospike Kubernetes Operator.

## Add Aerospike Prometheus Exporter Sidecar

Add the exporter as a sidecar to each Aerospike server pod using the [PodSpec configuration](Cluster-configuration-settings.md#pod-spec). For example,


```yaml
spec:
 .
 .
 .

  podSpec:
    sidecars:
      - name: aerospike-prometheus-exporter
        image: "aerospike/aerospike-prometheus-exporter:1.3.0"
        ports:
          - containerPort: 9145
            name: aerospike-prometheus-exporter

 .
 .
 .
```

## Deploy or Update Aerospike Cluster (Custom Resource)

[Create or update](Create-Aerospike-cluster.md) your clusters once the Prometheus exporter sidecar is added.

## Prometheus Configuration

Configure Prometheus to add exporter endpoints as scrape targets.

If Prometheus is also running on Kubernetes, it can be configured to extract exporter targets from Kubernetes API.

In the following example, Prometheus will be able to discover and add exporter targets in `default` namespace which has endpoint port name as `aerospike-prometheus-exporter`.

```yaml
scrape_configs:
  - job_name: 'aerospike'

    kubernetes_sd_configs:
    - role: endpoints
      namespaces:
        names:
        - default
    relabel_configs:
    - source_labels: [__meta_kubernetes_endpoint_port_name]
      separator: ;
      regex: aerospike-prometheus-exporter
      replacement: $1
      action: keep
```

See [Aerospike Monitoring Stack](https://docs.aerospike.com/docstools/monitorstack/index.md) for its installation, configuration and setup guide.

## Quick Example

This example demonstrates monitoring of Aerospike clusters deployed by Kubernetes Operator using Aerospike Monitoring Stack.

1. Add Aerospike helm repository
    ```sh
    helm repo add aerospike https://aerospike.github.io/aerospike-kubernetes-enterprise
    ```

2. Deploy Aerospike Kubernetes Operator using helm chart
    ```sh
    helm install operator aerospike/aerospike-kubernetes-operator --set replicas=1
    ```

3. Create a Kubernetes secret to store Aerospike license feature key file
    ```sh
    kubectl create secret generic aerospike-license --from-file=<path-to-features.conf-file>
    ```

4. Deploy Aerospike cluster with Aerospike Prometheus Exporter sidecar
    ```sh
    cat << EOF | helm install aerospike aerospike/aerospike-cluster \
    --set devMode=true \
    --set aerospikeSecretName=aerospike-license \
    -f -
    podSpec:
      sidecars:
      - name: aerospike-prometheus-exporter
        image: "aerospike/aerospike-prometheus-exporter:1.3.0"
        ports:
        - containerPort: 9145
          name: exporter
    EOF
    ```

5. Deploy Prometheus-Grafana Stack using [aerospike-monitoring-stack.yaml](https://docs.aerospike.com/docscloud/assets/aerospike-monitoring-stack.yaml)
    ```sh
    kubectl apply -f ./aerospike-monitoring-stack.yaml
    ```

6. Connect to Grafana dashboard,
    ```sh
    kubectl port-forward service/aerospike-monitoring-stack-grafana 3000:80
    ```
    Open `localhost:3000` in browser, and login to Grafana as `admin`/`admin`.

7. Import dashboards from [Aerospike Monitoring GitHub Repo](https://github.com/aerospike/aerospike-monitoring/tree/master/config/grafana/dashboards) and visualise metrics.
