---
title: Configure monitoring and alerting
menuTitle: Configure monitoring and alerting
description: setup monitoring and alerts for the Helm Chart
aliases:
  - /docs/installation/helm/monitoring
weight: 100
keywords:
  - monitoring
  - alert
  - alerting
---

# Configure monitoring and alerting

By default this Helm Chart configures meta-monitoring of metrics (service monitoring) and logs (self monitoring).

The `ServiceMonitor` resource works with either the Prometheus Operator or the Grafana Agent Operator, and defines how Loki's metrics should be scraped. Scraping this Loki cluster using the scrape config defined in the `SerivceMonitor` resource is required for the included dashboards to work. A `MetricsInstance` can be configured to write the metrics to a remote Prometheus instance such as Grafana Cloud Metrics.

_Self monitoring_ is enabled by default. This will deploy a `GrafanaAgent`, `LogsInstance`, and `PodLogs` resource which will instruct the Grafana Agent Operator (installed seperately) on how to scrape this Loki cluster's logs and send them back to itself. Scraping this Loki cluster using the scrape config defined in the `PodLogs` resource is required for the included dashboards to work.

Rules and alerts are automatically deployed.

**Before you begin:**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster with a running Loki deployment.
- A running Grafana instance.
- A running Prometheus Operator installed using the `kube-prometheus-stack` Helm chart.

**Prometheus Operator Prequisites**

The included dashboards require certain metric relabelings be applied to Kubernetes metrics. The best was to accomplish this is to install the `kube-prometheus-stack` helm chart with the following values file, replacing `CLUSTER_NAME` with the name of your cluster. The cluster name is what you specify during the helm installation, so a cluster installed with the command `helm install loki-cluster grafana/loki` would be called `loki-cluster`.

```yaml
kubelet:
  serviceMonitor:
    cAdvisorRelabelings:
      - action: replace
        replacement: <CLUSTER_NAME>
        targetLabel: cluster
      - targetLabel: metrics_path
        sourceLabels:
          - "__metrics_path__"
      - targetLabel: "instance"
        sourceLabels:
          - "node"

defaultRules:
  additionalRuleLabels:
    cluster: <CLUSTER_NAME>

"kube-state-metrics":
  prometheus:
    monitor:
      relabelings:
        - action: replace
          replacement: <CLUSTER_NAME>
          targetLabel: cluster
        - targetLabel: "instance"
          sourceLabels:
            - "__meta_kubernetes_pod_node_name"

"prometheus-node-exporter":
  prometheus:
    monitor:
      relabelings:
        - action: replace
          replacement: <CLUSTER_NAME>
          targetLabel: cluster
        - targetLabel: "instance"
          sourceLabels:
            - "__meta_kubernetes_pod_node_name"

prometheus:
  monitor:
    relabelings:
      - action: replace
        replacement: <CLUSTER_NAME>
        targetLabel: cluster
```

The `kube-prometheus-stack` installs `ServicMonitor` and `PrometheusRule` resources for monitoring Kubernetes, and it depends on the `kube-state-metrics` and `prometheus-node-exporter` helm charts which also install `ServiceMonitor` resources for collecting `kubelet` and `node-exporter` metrics. The above values file adds the necessary additional relabelings are required for these metrics to work with the included dashboards.

If using this helm chart in an environment which does not allow for the installation of `kube-prometheus-stack` or custom CRDs, it is recommended you run `helm template` on the `kube-prometheus-stack` helm chart with the above values file, and grab all generated `ServiceMonitor` and `PrometheusRule` resources. Note those resources may have to be modified with the correct ports and selectors to find the various services such as `kubelet` and `node-exporter` in your environment.

**To install the dashboards:**

1. Dashboards are enabled by default. Set `monitoring.dashboards.namespace` to the namespace of the Grafana instance if it is in a different namespace than this Loki cluster.

**To add add additional Prometheus rules:**

1. Modify the configuration file `values.yaml`:

   ```yaml
   monitoring:
     rules:
       additionalGroups:
         - name: loki-rules
           rules:
             - record: job:loki_request_duration_seconds_bucket:sum_rate
               expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job)
             - record: job_route:loki_request_duration_seconds_bucket:sum_rate
               expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job, route)
             - record: node_namespace_pod_container:container_cpu_usage_seconds_total:sum_rate
               expr: sum(rate(container_cpu_usage_seconds_total[1m])) by (node, namespace, pod, container)
   ```

**To disable monitoring:**

1. Modify the configuration file `values.yaml`:

   ```yaml
   selfMonitoring:
     enabled: false

   serviceMonitor:
     enabled: false
   ```

**To use a remote Prometheus and Loki instance such as Grafana Cloud**

1. Create a `secrets.yaml` file with credentials to access the Grafana Cloud services:

   ```yaml
   ---
   apiVersion: v1
   kind: Secret
   metadata:
     name: primary-credentials-metrics
     namespace: default
   stringData:
     username: "<instance ID>"
     password: "<API key>"
   ---
   apiVersion: v1
   kind: Secret
   metadata:
     name: primary-credentials-logs
     namespace: default
   stringData:
     username: "<instance ID>"
     password: "<API key>"
   ```

2. Add the secret to Kubernetes with `kubectl create -f secret.yaml`.

3. Add a `remoteWrite` section to `serviceMonitor` in `values.yaml`:

   ```yaml
   monitoring:
   ...
     serviceMonitor:
       enabled: true
       ...
       metricsInstance:
         remoteWrite:
         - url: <metrics remote write endpoint>
           basicAuth:
             username:
               name: primary-credentials-metrics
               key: username
             password:
               name: primary-credentials-metrics
               key: password
   ```

4. Add a client to `monitoring.selfMonitoring.logsInstance.clients`:

   ```yaml
   monitoring:
   ---
   selfMonitoring:
     enabled: true
     logsInstance:
       clients:
         - url: <logs remote write endpoint>
           basicAuth:
             username:
               name: primary-credentials-logs
               key: username
             password:
               name: primary-credentials-logs
               key: password
   lokiCanary:
     enabled: false
   ```

5. Install the `Loki meta-motoring` connection on Grafana Cloud.
