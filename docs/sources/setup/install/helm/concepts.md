---
title: Helm chart components
menuTitle: Helm chart components
description: A short introduction to the components installed with the Loki Helm Chart.
aliases:
  - ../../../installation/helm/concepts/
weight: 100
keywords:
  - dashboards
  - gateway
  - caching
---

# Helm chart components

This section describes the components installed by the Helm Chart.

## 3 methods of deployment

The Loki chart supports three methods of deployment:
- [Monolithic](../install-monolithic/) 
- [Simple Scalable](../install-scalable/)
- [Microservice](../install-microservices/)

By default, the chart installs in [Simple Scalable](../install-scalable/) mode. For the best possible experience in production, we now recommend deploying Loki in *microservices* mode. To understand the differences between deployment methods, see the [Loki deployment modes](../../../../get-started/deployment-modes/) documentation.

{{< admonition type="note" >}}
Simple Scalable Deployment (SSD) mode is being deprecated. The timeline for the deprecation is to be determined (TBD), but will happen before Loki 4.0 is released.
{{< /admonition >}}

## Zone-aware replication

When deploying in [microservices](../install-microservices/) mode, the chart enables **zone-aware replication** for ingesters by default (`ingester.zoneAwareReplication.enabled: true`). This creates three ingester StatefulSets (zone-a, zone-b, zone-c) and requires the `rollout-operator` subchart. Zone-aware replication allows multiple ingesters within a single zone to be shut down and restarted simultaneously during rollouts, while the remaining two zones guarantee at least one copy of the data.

To disable zone-aware replication (for example, in a development or test environment):

```yaml
ingester:
  zoneAwareReplication:
    enabled: false
```

## Pattern ingester

The chart includes an optional **pattern ingester** component (`patternIngester`) for detecting and extracting log patterns. It is disabled by default (`patternIngester.replicas: 0`). To enable it as a standalone component in microservices mode, set `patternIngester.replicas` to a non-zero value. In other modes, enable it in the Loki configuration:

```yaml
loki:
  pattern_ingester:
    enabled: true
```

## Overrides exporter

The **overrides exporter** (`overridesExporter`) is an optional component that exposes per-tenant configuration overrides as Prometheus metrics. It is disabled by default (`overridesExporter.enabled: false`). Enable it when you need visibility into tenancy-level limit settings.

## Bloom filters (experimental)

The chart includes experimental support for **bloom filters** through three components:
- `bloomGateway`: Serves bloom filter queries
- `bloomPlanner`: Plans bloom filter build jobs
- `bloomBuilder`: Builds bloom filters

All three are disabled by default (replicas set to 0). Enable bloom filters in the Loki configuration (`loki.bloom_build.enabled: true`, `loki.bloom_gateway.enabled: true`) and set non-zero replica counts for the corresponding components.

## Monitoring Loki

The Loki Helm chart includes built-in monitoring resources that can be enabled:
- **ServiceMonitor** (`monitoring.serviceMonitor.enabled`): Creates Prometheus Operator ServiceMonitor resources for scraping Loki metrics.
- **Recording rules and alerts** (`monitoring.rules.enabled`): Creates PrometheusRule resources with pre-configured recording rules and alerts (for example, `LokiRequestErrors`, `LokiRequestPanics`, `LokiRequestLatency`).
- **Dashboards** (`monitoring.dashboards.enabled`): Creates ConfigMaps containing Grafana dashboards for monitoring Loki.

These built-in monitoring resources are disabled by default. For a more comprehensive monitoring setup, Loki clusters can also be monitored using the meta-monitoring stack, which monitors the logs, metrics, and traces of the Loki cluster. There are two deployment options for this stack, see the installation instructions within [Monitoring](../monitor-and-alert/).

{{< admonition type="note" >}}
The Kubernetes Monitoring Helm chart replaces the monitoring section of the Loki Helm chart which is now **DEPRECATED**. Refer to the [Monitoring](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/) section for more information.
{{< /admonition >}}

## Canary

This chart installs the [Loki Canary app](../../../../operations/loki-canary/) by default. This is another tool to verify the Loki deployment is in a healthy state. It can be disabled by setting `lokiCanary.enabled=false`.

## Gateway

By default and inspired by Grafana's [Tanka setup](https://github.com/grafana/loki/blob/main/production/ksonnet/loki), the chart
installs the gateway component which is an NGINX that exposes the Loki API and automatically proxies requests to the correct
Loki components (read or write, or single instance in the case of filesystem storage).
The gateway must be enabled if an Ingress is required, since the Ingress exposes the gateway only.
If the gateway is enabled, Grafana and log shipping agents, such as Grafana Alloy, should be configured to use the gateway.
If NetworkPolicies are enabled, they are more restrictive if the gateway is enabled.

## Caching

By default, this chart configures in-memory caching. If that caching does not work for your deployment, you should setup [memcache](../../../../operations/caching/).
