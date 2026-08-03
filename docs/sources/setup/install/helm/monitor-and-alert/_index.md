---
title: Monitoring
description: Describes monitoring options for Loki deployed with Helm, including built-in Prometheus Operator resources and the recommended Kubernetes monitoring Helm chart.
weight: 500
aliases:
  - ../../../installation/helm/monitor-and-alert/
keywords:
  - helm 
  - scalable
  - simple-scalable
  - monitor
---

# Monitoring

## Built-in monitoring (Prometheus Operator)

The Loki Helm chart can render ServiceMonitors, PrometheusRule recording rules, PrometheusRule alerts, and Grafana dashboards from the upstream [loki-mixin](https://github.com/grafana/loki/tree/main/production/loki-mixin):

```yaml
monitoring:
  serviceMonitor:
    enabled: true
  rules:
    enabled: true   # recording rules only
  alerts:
    enabled: true   # alert rules (separate since chart 18.0.0)
  dashboards:
    enabled: true
```

All monitoring resources are disabled by default.

As of chart 18.0.0, alert rules are managed under `monitoring.alerts`, not `monitoring.rules` (which now controls recording rules only). The release-identifying metric label is `app_instance` by default (`monitoring.appInstanceLabelName`).

For details on available monitoring keys, refer to the [Helm Chart Reference](../reference/).

## Recommended: Kubernetes monitoring Helm chart

<!-- vale Grafana.We = NO -->
{{< admonition type="warning" >}}
We no longer recommend using the meta-monitoring Helm chart to monitor Loki. The former `monitoring.selfMonitoring` / Grafana Agent integration was removed in chart 9.0.0. To consolidate monitoring efforts into one Helm chart, Grafana Labs recommends using the [Kubernetes monitoring Helm chart](https://github.com/grafana/k8s-monitoring-helm). Instructions for setting up the Kubernetes monitoring Helm chart can be found under [Manage](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/).
{{< /admonition >}}
<!-- vale Grafana.We = YES -->
