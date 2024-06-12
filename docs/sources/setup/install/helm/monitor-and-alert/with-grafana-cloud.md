---
title: Monitor Loki with Grafana Cloud
menuTitle: Monitor Loki with Grafana Cloud
description: Configuring monitoring for Loki using Grafana Cloud.
aliases:
  - ../../../../installation/helm/monitor-and-alert/with-grafana-cloud
weight: 200
keywords:
  - monitoring
  - alert
  - alerting
  - grafana cloud
---

# Monitor Loki with Grafana Cloud

This guide will walk you through using Grafana Cloud to monitor a Loki installation set up with the `meta-monitoring` Helm chart. This method takes advantage of many of the chart's self-monitoring features, sending metrics, logs, and traces from the Loki deployment to Grafana Cloud. Monitoring Loki with Grafana Cloud offers the added benefit of troubleshooting Loki issues even when the Helm-installed Loki is down, as the telemetry data will remain available in the Grafana Cloud instance.

These instructions are based off the [meta-monitoring-chart repository](https://github.com/grafana/meta-monitoring-chart/tree/main).

## Before you begin

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A Grafana Cloud account and stack (including Cloud Grafana, Cloud Metrics, and Cloud Logs).
- A running Loki deployment installed in that Kubernetes cluster via the Helm chart.

## Configure the meta namespace

The meta-monitoring stack will be installed in a separate namespace called `meta`. To create this namespace, run the following command:

  ```bash
    kubectl create namespace meta
  ```
  
## Grafana Cloud Connection Credentials

The meta-monitoring stack sends metrics, logs, and traces to Grafana Cloud. This requires that you know your connection credentials to Grafana Cloud. To obtain connection credentials, follow the steps below:

1. Create a new Cloud Access Policy in Grafana Cloud.     
    1. Sign into [Grafana Cloud](https://grafana.com/auth/sign-in/).
    1. In the main menu, select **Security > Access Policies**.
    1. Click **Create access policy**.
    1. Give the policy a **Name** and select the following permissions:
       - Metrics: Write
       - Logs: Write
       - Traces: Write
  1. Click **Create**.


1. Once the policy is created, select the policy and click  **Add token**. 
1. Name the token, select an expiration date, then click **Create**. 
1. Copy the token to a secure location as it will not be displayed again.

1. Navigate to the Grafana Cloud Portal **Overview** page.
1. Click the **Details** button for your Prometheus or Mimir instance.
     1. From the **Using a self-hosted Grafana instance with Grafana Cloud Metrics** section, collect the instance **Name** and **URL**.
     1. Navigate back to the **Overview** page.
1. Click the **Details** button for your Loki instance.
     1. From the **Using Grafana with Logs** section, collect the instance **Name** and **URL**.
     1. Navigate back to the **Overview** page.
1. Click the **Details** button for your Tempo instance.
    1. From the **Using Grafana with Tempo** section, collect the instance **Name** and **URL**.

3. Finally, generate the secrets to store your credentials for each metric type within your Kubernetes cluster:
   ```bash
      kubectl create secret generic logs -n meta \
        --from-literal=username=<USERNAME LOGS> \
        --from-literal= <ACCESS POLICY TOKEN> \
        --from-literal=endpoint='https://<LOG URL>/loki/api/v1/push'

        kubectl create secret generic metrics -n meta \
        --from-literal=username=<USERNAME METRICS> \
        --from-literal=password=<ACCESS POLICY TOKEN> \
        --from-literal=endpoint='https://<METRICS URL>/api/prom/push'

        kubectl create secret generic traces -n meta \
        --from-literal=username=<OTLP INSTANCE ID> \
        --from-literal=password=<ACCESS POLICY TOKEN> \
        --from-literal=endpoint='https://<OTLP URL>/otlp'
   ```

## Configuration and Installation

To install the `meta-monitoring` Helm chart, you must create a `values.yaml` file. At a minimum this file should contain the following:
  * The namespace to monitor
  * Enablement of cloud monitoring

This example `values.yaml` file provides the minimum configuration to monitor the `loki` namespace:

```yaml
  namespacesToMonitor:
  - default

  cloud:
    logs:
      enabled: true
      secret: "logs"
    metrics:
      enabled: true
      secret: "metrics"
    traces:
      enabled: true
      secret: "traces"
```
For further configuration options, refer to the [sample values.yaml file](https://github.com/grafana/meta-monitoring-chart/blob/main/charts/meta-monitoring/values.yaml).

To install the `meta-monitoring` Helm chart, run the following commands:

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
helm install meta-monitoring grafana/meta-monitoring -n meta -f values.yaml 
```
or when upgrading the configuration:
```bash
helm upgrade meta-monitoring grafana/meta-monitoring -n meta -f values.yaml 
```

To verify the installation, run the following command:

```bash
kubectl get pods -n meta
```
It should return the following pods:
```bash
NAME           READY   STATUS    RESTARTS   AGE
meta-alloy-0   2/2     Running   0          23h
meta-alloy-1   2/2     Running   0          23h
meta-alloy-2   2/2     Running   0          23h
```


## Enable Loki Tracing

By default, Loki does not have tracing enabled. To enable tracing, modify the Loki configuration by editing the `values.yaml` file and adding the following configuration:

Set the `tracing.enabled` configuration to `true`:
```yaml
loki:
  tracing:
    enabled: true
```

Next, instrument each of the Loki components to send traces to the meta-monitoring stack. Add the `extraEnv` configuration to each of the Loki components:

```yaml
ingester:
  replicas: 3
  extraEnv:
    - name: JAEGER_ENDPOINT
      value: "http://mmc-alloy-external.default.svc.cluster.local:14268/api/traces"
      # This sets the Jaeger endpoint where traces will be sent.
      # The endpoint points to the mmc-alloy service in the default namespace at port 14268.
      
    - name: JAEGER_AGENT_TAGS
      value: 'cluster="prod",namespace="default"'
      # This specifies additional tags to attach to each span.
      # Here, the cluster is labeled as "prod" and the namespace as "default".
      
    - name: JAEGER_SAMPLER_TYPE
      value: "ratelimiting"
      # This sets the sampling strategy for traces.
      # "ratelimiting" means that traces will be sampled at a fixed rate.
      
    - name: JAEGER_SAMPLER_PARAM
      value: "1.0"
      # This sets the parameter for the sampler.
      # For ratelimiting, "1.0" typically means one trace per second.
```

Since the meta-monitoring stack is installed in the `meta` namespace, the Loki components will need to be able to communicate with the meta-monitoring stack. To do this, create a new `externalname` service in the `default` namespace that points to the `meta` namespace by running the following command:

```bash
kubectl create service externalname mmc-alloy-external --external-name meta-alloy.meta.svc.cluster.local -n default
```

Finally, upgrade the Loki installation with the new configuration:

```bash
helm upgrade --values values.yaml loki grafana/loki
```

## Import the Loki Dashboards to Grafana Cloud

The meta-monitoring stack includes a set of dashboards that can be imported into Grafana Cloud. These can be found in the [meta-monitoring repository](https://github.com/grafana/meta-monitoring-chart/tree/main/charts/meta-monitoring/src/dashboards).


## Installing Rules

The meta-monitoring stack includes a set of rules that can be installed to monitor the Loki installation. These rules can be found in the [meta-monitoring repository](https://github.com/grafana/meta-monitoring-chart/). To install the rules:

1. Clone the repository:
   ```bash
   git clone https://github.com/grafana/meta-monitoring-chart/
   ```
1. Install `mimirtool` based on the instructions located [here](https://grafana.com/docs/mimir/latest/manage/tools/mimirtool/)
1. Create a new access policy token in Grafana Cloud with the following permissions:
   - Rules: Write
   - Rules: Read
1. Create a token for the access policy and copy it to a secure location.
1. Install the rules:
   ```bash
   mimirtool rules load --address=<your_cloud_prometheus_endpoint> --id=<your_instance_id> --key=<your_cloud_access_policy_token> *.yaml
   ```
1. Verify that the rules have been installed:
   ```bash
    mimirtool rules list --address=<your_cloud_prometheus_endpoint> --id=<your_instance_id> --key=<your_cloud_access_policy_token>
    ```
   It should return a list of rules that have been installed.
   ```bash

   loki-rules:
    - name: loki_rules
      rules:
        - record: cluster_job:loki_request_duration_seconds:99quantile
          expr: histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, job))
        - record: cluster_job:loki_request_duration_seconds:50quantile
          expr: histogram_quantile(0.50, sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, job))
        - record: cluster_job:loki_request_duration_seconds:avg
          expr: sum(rate(loki_request_duration_seconds_sum[5m])) by (cluster, job) / sum(rate(loki_request_duration_seconds_count[5m])) by (cluster, job)
        - record: cluster_job:loki_request_duration_seconds_bucket:sum_rate
          expr: sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, job)
        - record: cluster_job:loki_request_duration_seconds_sum:sum_rate
          expr: sum(rate(loki_request_duration_seconds_sum[5m])) by (cluster, job)
        - record: cluster_job:loki_request_duration_seconds_count:sum_rate
          expr: sum(rate(loki_request_duration_seconds_count[5m])) by (cluster, job)
        - record: cluster_job_route:loki_request_duration_seconds:99quantile
          expr: histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, job, route))
        - record: cluster_job_route:loki_request_duration_seconds:50quantile
          expr: histogram_quantile(0.50, sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, job, route))
        - record: cluster_job_route:loki_request_duration_seconds:avg
          expr: sum(rate(loki_request_duration_seconds_sum[5m])) by (cluster, job, route) / sum(rate(loki_request_duration_seconds_count[5m])) by (cluster, job, route)
        - record: cluster_job_route:loki_request_duration_seconds_bucket:sum_rate
          expr: sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, job, route)
        - record: cluster_job_route:loki_request_duration_seconds_sum:sum_rate
          expr: sum(rate(loki_request_duration_seconds_sum[5m])) by (cluster, job, route)
        - record: cluster_job_route:loki_request_duration_seconds_count:sum_rate
          expr: sum(rate(loki_request_duration_seconds_count[5m])) by (cluster, job, route)
        - record: cluster_namespace_job_route:loki_request_duration_seconds:99quantile
          expr: histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, namespace, job, route))
        - record: cluster_namespace_job_route:loki_request_duration_seconds:50quantile
          expr: histogram_quantile(0.50, sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, namespace, job, route))
        - record: cluster_namespace_job_route:loki_request_duration_seconds:avg
          expr: sum(rate(loki_request_duration_seconds_sum[5m])) by (cluster, namespace, job, route) / sum(rate(loki_request_duration_seconds_count[5m])) by (cluster, namespace, job, route)
        - record: cluster_namespace_job_route:loki_request_duration_seconds_bucket:sum_rate
          expr: sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, cluster, namespace, job, route)
        - record: cluster_namespace_job_route:loki_request_duration_seconds_sum:sum_rate
          expr: sum(rate(loki_request_duration_seconds_sum[5m])) by (cluster, namespace, job, route)
        - record: cluster_namespace_job_route:loki_request_duration_seconds_count:sum_rate
          expr: sum(rate(loki_request_duration_seconds_count[5m])) by (cluster, namespace, job, route)
   ```
## Install kube-state-metrics

Metrics about Kubernetes objects are scraped from [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics). This needs to be installed in the cluster. The `kubeStateMetrics.endpoint` entry in the meta-monitoring `values.yaml` should be set to its address (without the `/metrics` part in the URL):

```yaml
kubeStateMetrics:
  # Scrape https://github.com/kubernetes/kube-state-metrics by default
  enabled: true
  # This endpoint is created when the helm chart from
  # https://artifacthub.io/packages/helm/prometheus-community/kube-state-metrics/
  # is used. Change this if kube-state-metrics is installed somewhere else.
  endpoint: kube-state-metrics.kube-state-metrics.svc.cluster.local:8080
```


