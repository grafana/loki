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

By default, the chart installs in [Monolithic](../install-monolithic/) mode (`deploymentMode: Monolithic`). For production at scale, we recommend deploying Loki in *microservices* (`deploymentMode: Distributed`) mode. To understand the differences between deployment methods, see the [Loki deployment modes](../../../../get-started/deployment-modes/) documentation.

{{< admonition type="note" >}}
Simple Scalable Deployment (SSD) mode is being deprecated. The timeline for the deprecation is to be determined (TBD), but will happen before Loki 4.0 is released.
{{< /admonition >}}

## Zone-aware replication

When deploying in [microservices](../install-microservices/) mode, the chart enables **zone-aware replication** for ingesters by default (`ingester.zoneAwareReplication.enabled: true`). This creates three ingester StatefulSets (zone-a, zone-b, zone-c) and requires enabling the `rollout-operator` subchart (`rollout_operator.enabled: true`) for coordinated zone rollouts. Zone-aware replication allows multiple ingesters within a single zone to be shut down and restarted simultaneously during rollouts, while the remaining two zones guarantee at least one copy of the data.

To disable zone-aware replication (for example, in a development or test environment):

```yaml
ingester:
  zoneAwareReplication:
    enabled: false
```

## Pattern ingester

The chart includes an optional **pattern ingester** component (`patternIngester`) for detecting and extracting log patterns. It is disabled by default (`patternIngester.enabled: false`, `replicas: 0`). In Distributed mode (`deploymentMode: Distributed`), enable the standalone workload with:

```yaml
deploymentMode: Distributed
patternIngester:
  enabled: true
  replicas: 1
loki:
  pattern_ingester:
    enabled: true
```

In Monolithic or SimpleScalable mode, enable it via Loki configuration only:

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
- **Recording rules** (`monitoring.rules.enabled`): Creates a PrometheusRule resource with loki-mixin recording rules.
- **Alert rules** (`monitoring.alerts.enabled`): Creates a PrometheusRule resource with alerts such as `LokiRequestErrors`, `LokiRequestPanics`, and `LokiRequestLatency`.
- **Dashboards** (`monitoring.dashboards.enabled`): Creates ConfigMaps containing Grafana dashboards for monitoring Loki.

These built-in monitoring resources are disabled by default. For comprehensive cluster-wide observability, use the [Kubernetes monitoring Helm chart](https://github.com/grafana/k8s-monitoring-helm). See [Monitoring](../monitor-and-alert/) for details.

{{< admonition type="note" >}}
For comprehensive cluster-wide observability, Grafana Labs recommends the [Kubernetes monitoring Helm chart](https://github.com/grafana/k8s-monitoring-helm). The Loki chart still provides optional built-in Prometheus Operator resources under `monitoring.*`; only the former `monitoring.selfMonitoring` / Grafana Agent integration has been removed.
{{< /admonition >}}

## Canary

This chart installs the [Loki Canary app](../../../../operations/loki-canary/) by default. This is another tool to verify the Loki deployment is in a healthy state. It can be disabled by setting `lokiCanary.enabled=false`.

## Gateway

By default and inspired by Grafana's [Tanka setup](https://github.com/grafana/loki/blob/main/production/ksonnet/loki), the chart
installs the gateway component which is an NGINX that exposes the Loki API and automatically proxies requests to the correct
Loki components (read or write, or single instance in the case of filesystem storage).
You can expose Loki either through `gateway.ingress` (with `gateway.enabled: true`) or through the top-level `ingress` key (with `gateway.enabled: false`), but not both.
If the gateway is enabled, Grafana and log shipping agents, such as Grafana Alloy, should be configured to use the gateway.
If NetworkPolicies are enabled, they are more restrictive if the gateway is enabled.

## Caching

By default, the chart deploys Memcached-based **chunks cache** (`chunksCache.enabled: true`) and **results cache** (`resultsCache.enabled: true`). To use an externally managed Memcached instead, disable the built-in caches and point `chunksCache.addresses` / `resultsCache.addresses` at your service. See [caching](../../../../operations/caching/) for tuning guidance.
