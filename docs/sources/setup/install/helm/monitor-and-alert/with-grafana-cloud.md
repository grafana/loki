---
title: Configure monitoring and alerting of Loki using Grafana Cloud
menuTitle: Monitor Loki with Grafana Cloud
description: setup monitoring and alerts for Loki using Grafana Cloud
aliases:
  - ../../../../installation/helm/monitor-and-alert/with-grafana-cloud
weight: 200
keywords:
  - monitoring
  - alert
  - alerting
  - grafana cloud
---

# Configure monitoring and alerting of Loki using Grafana Cloud

This topic will walk you through using Grafana Cloud to monitor a Loki installation that is installed with the Helm chart. This approach leverages many of the chart's _self monitoring_ features, but instead of sending logs back to Loki itself, it sends them to a Grafana Cloud Logs instance. This approach also does not require the installation of the Prometheus Operator and instead sends metrics to a Grafana Cloud Metrics instance. Using Grafana Cloud to monitor Loki has the added benefit of being able to troubleshoot problems with Loki when the Helm installed Loki is down, as the logs will still be available in the Grafana Cloud Logs instance.

**Before you begin:**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A Grafana Cloud account and stack (including Cloud Grafana, Cloud Metrics, and Cloud Logs).
- [Grafana Kubernetes Monitoring using Agent Flow](/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/configuration/config-k8s-agent-flow/) configured for the Kubernetes cluster.
- A running Loki deployment installed in that Kubernetes cluster via the Helm chart.

**Prequisites for Monitoring Loki:**

You must setup the Grafana Kubernetes Integration following the instructions in [Grafana Kubernetes Monitoring using Agent Flow](/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/configuration/config-k8s-agent-flow/) as this will install necessary components for collecting metrics about your Kubernetes cluster and sending them to Grafana Cloud. Many of the dashboards installed as a part of the Loki integration rely on these metrics.

Walking through this installation will create two Grafana Agent configurations, one for metrics and one for logs, that will add the external label `cluster: cloud`. In order for the Dashboards in the self-hosted Grafana Loki integration to work, the cluster name needs to match your Helm installation name. If you installed Loki using the command `helm install best-loki-cluster grafana/loki`, you would need to change the `cluster` value in both Grafana Agent configurations from `cloud` to `best-loki-cluster` when setting up the Grafana Kubernetes integration.

**To set up the Loki integration in Grafana Cloud:**

1. Get valid Push credentials for your Cloud Metrics and Cloud Logs instances.
1. Create a secret in the same namespace as Loki to store your Cloud Logs credentials.

   ```bash
   cat <<'EOF' | NAMESPACE=loki /bin/sh -c 'kubectl apply -n $NAMESPACE -f -'
   apiVersion: v1
   data:
     password: <BASE64_ENCODED_CLOUD_LOGS_PASSWORD>
     username: <BASE64_ENCODED_CLOUD_LOGS_USERNAME>
   kind: Secret
   metadata:
     name: grafana-cloud-logs-credentials
   type: Opaque
   EOF
   ```

1. Create a secret to store your Cloud Metrics credentials.

   ```bash
   cat <<'EOF' | NAMESPACE=loki /bin/sh -c 'kubectl apply -n $NAMESPACE -f -'
   apiVersion: v1
   data:
     password: <BASE64_ENCODED_CLOUD_METRICS_PASSWORD>
     username: <BASE64_ENCODED_CLOUD_METRICS_USERNAME>
   kind: Secret
   metadata:
     name: grafana-cloud-metrics-credentials
   type: Opaque
   EOF
   ```

1. Enable monitoring metrics and logs for the Loki installation to be sent your cloud database instances by adding the following to your Helm `values.yaml` file:

   ```yaml
   ---
   monitoring:
     dashboards:
       enabled: false
     rules:
       enabled: false
     selfMonitoring:
       logsInstance:
         clients:
           - url: <CLOUD_LOGS_URL>
             basicAuth:
               username:
                 name: grafana-cloud-logs-credentials
                 key: username
               password:
                 name: grafana-cloud-logs-credentials
                 key: password
     serviceMonitor:
       metricsInstance:
         remoteWrite:
           - url: <CLOUD_METRICS_URL>
             basicAuth:
               username:
                 name: grafana-cloud-metrics-credentials
                 key: username
               password:
                 name: grafana-cloud-metrics-credentials
                 key: password
   ```

1. Install the self-hosted Grafana Loki integration by going to your hosted Grafana instance, selecting **Connections** from the Home menu, then search for and install the **Self-hosted Grafana Loki** integration.

1. Once the self-hosted Grafana Loki integration is installed, click the **View Dashboards** button to see the installed dashboards.
