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

By default, the chart installs in [Simple Scalable](../install-scalable/) mode. This is the recommended method for most users. To understand the differences between deployment methods, see the [Loki deployment modes](../../../../get-started/deployment-modes/) documentation.

## Monitoring Loki

The Loki Helm chart does not deploy self-monitoring by default. Loki clusters can be monitored using the meta-monitoring stack, which monitors the logs, metrics, and traces of the Loki cluster. There are two deployment options for this stack, see the installation instructions within [Monitoring](../monitor-and-alert/).

{{< admonition type="note" >}}
The meta-monitoring stack replaces the monitoring section of the Loki helm chart which is now **DEPRECATED**. See the [Monitoring](../monitor-and-alert/) section for more information.
{{< /admonition >}}


## Canary

This chart installs the [Loki Canary app](../../../../operations/loki-canary/) by default. This is another tool to verify the Loki deployment is in a healthy state. It can be disabled by setting `lokiCanary.enabled=false`.

## Gateway

By default and inspired by Grafana's [Tanka setup](https://github.com/grafana/loki/blob/main/production/ksonnet/loki), the chart
installs the gateway component which is an NGINX that exposes the Loki API and automatically proxies requests to the correct
Loki components (read or write, or single instance in the case of filesystem storage).
The gateway must be enabled if an Ingress is required, since the Ingress exposes the gateway only.
If the gateway is enabled, Grafana and log shipping agents, such as Promtail, should be configured to use the gateway.
If NetworkPolicies are enabled, they are more restrictive if the gateway is enabled.

## Caching

By default, this chart configures in-memory caching. If that caching does not work for your deployment, you should setup [memcache](../../../../operations/caching/).
