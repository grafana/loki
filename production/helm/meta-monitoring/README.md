# Meta-monitoring Loki - Kubernetes Helm Chart

This Helm chart provides comprehensive monitoring for Loki deployments in Kubernetes, based on the [Grafana Kubernetes Monitoring Helm Chart](https://github.com/grafana/k8s-monitoring-helm/tree/main).

## Overview

The Meta-monitoring chart collects metrics and logs from Loki deployments using Grafana Alloy (an OpenTelemetry Collector distribution) and sends them to your preferred observability backend. It's designed to work with the pre-compiled Loki mixins to provide dashboards and alerts specifically tailored for Loki monitoring.

## Prerequisites

- [kubectl](https://kubernetes.io/docs/reference/kubectl/)
- Kubernetes cluster
- Helm 3.x
- Access to Grafana Cloud or a self-managed Prometheus/Loki/Grafana stack
- For self-managed destinations: credentials stored in Kubernetes secrets

## Installation

1. Create the required secrets for your observability backend:

```bash
# For Grafana Cloud or self-managed stack
kubectl create namespace meta
kubectl create secret generic metrics --namespace meta \
  --from-literal=username='YOUR_PROMETHEUS_USERNAME' \
  --from-literal=password='YOUR_PROMETHEUS_PASSWORD'

kubectl create secret generic logs --namespace meta \
  --from-literal=username='YOUR_LOKI_USERNAME' \
  --from-literal=password='YOUR_LOKI_PASSWORD'
```

2. Install the Helm chart:

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm install meta-loki grafana/k8s-monitoring \
  --namespace meta \
  -f values.yaml
```

## Configuration

The default configuration is set up for Grafana Cloud, but you can direct it to your self-managed monitoring stack by modifying the destinations in `values.yaml`:

```yaml
destinations:
  - name: prometheus
    type: prometheus
    url:  https://<PROMETHEUS-ENDPOINT>/api/prom/push
    # Configure authentication as needed
    
  - name: loki
    type: loki
    url: https://<LOKI-ENDPOINT>/loki/api/v1/push
    # Configure authentication as needed
```

Note: Authentication is based on a pre-configured secret. The Helm chart does support more advanced authentication methods. Examples are provided in the [k8s-monitoring documentation](https://github.com/grafana/k8s-monitoring-helm/tree/main/charts/k8s-monitoring/docs/examples/auth)

## Key Features

- **Cluster Event Collection**: Captures Kubernetes events as logs
- **Metrics Collection**: Uses cadvisor, kubelet, and kube-state-metrics
- **Pod Log Collection**: Collects logs from Loki pods
- **Integrated Dashboards**: Works with pre-compiled Loki mixin dashboards
- **Alert Rules**: Pre-configured alerting rules for Loki components

## Loki Mixin Compiled Files

This chart is designed to work with the Loki mixins found in the `loki-mixin-compiled` directory, which contains:

### Files Overview

- **dashboards/**: Pre-compiled Grafana dashboards for visualizing Loki performance and health
- **alerts.yaml**: Pre-configured alerting rules for Loki components
- **rules.yaml**: Recording rules that create metrics used by dashboards and alerts

### Using the Dashboards

The `dashboards/` directory contains JSON files that can be imported directly into Grafana:

1. From your Grafana UI, go to **Dashboards → Import**.
1. Upload the JSON file or paste its contents.
1. Configure the data source (should match your Prometheus).
1. Click **Import**.

Alternatively, use the Grafana API to programmatically import dashboards:

```bash
# Example using curl to import a dashboard
curl -X POST -H "Content-Type: application/json" -H "Authorization: Bearer YOUR_API_KEY" \
  -d @path/to/dashboard.json \
  https://your-grafana-instance/api/dashboards/db
```

### Loading Alert and Recording Rules

For Grafana Cloud Prometheus or Grafana Mimir, use `[mimirtool](https://grafana.com/docs/mimir/latest/manage/tools/mimirtool/#installation)` to load the rules:

```bash
# Install mimirtool
go install github.com/grafana/mimir/pkg/mimirtool@latest

# Load alert rules
mimirtool rules load alerts.yaml \
  --address=https://prometheus-prod-xxx.grafana.net/api/prom \
  --id=<Your-Stack-ID> \
  --key=<Your-API-Key>

# Load recording rules
mimirtool rules load rules.yaml \
  --address=https://prometheus-prod-xxx.grafana.net/api/prom \
  --id=<Your-Stack-ID> \
  --key=<Your-API-Key>
```

For self-managed Prometheus:

```bash
# For Prometheus
cp rules.yaml alerts.yaml /etc/prometheus/rules/
# Then reload Prometheus configuration
curl -X POST http://prometheus:9090/-/reload
```

## Components Monitored

The meta-monitoring chart monitors:

- Loki components via label selection (`app.kubernetes.io/name: loki`)
- Alloy collectors in the `meta` namespace
- Kubernetes resources in the `loki` and `meta` namespaces

## Advanced Configuration

See the [k8s-monitoring documentation](https://github.com/grafana/k8s-monitoring-helm/tree/main/charts/k8s-monitoring/docs) for additional configuration options for:

- Authentication methods
- Collection tuning
- Metric processing
- Log processing

## References

- [Grafana Kubernetes Monitoring](https://github.com/grafana/k8s-monitoring-helm/tree/main)
- [Alloy integration](https://github.com/grafana/k8s-monitoring-helm/blob/main/charts/k8s-monitoring/charts/feature-integrations/docs/integrations/alloy.md)
- [Loki integration](https://github.com/grafana/k8s-monitoring-helm/blob/main/charts/k8s-monitoring/charts/feature-integrations/docs/integrations/loki.md)
- [Mimirtool documentation](https://grafana.com/docs/mimir/latest/manage/tools/mimirtool/)
