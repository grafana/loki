---
title: Helm chart components
menuTitle: Helm chart components
description: A short introduction to the components installed with the Loki Helm Chart.
aliases:
  - /docs/installation/helm/concepts
weight: 100
keywords:
  - dashboards
  - gateway
  - caching
---

# Helm chart components

This section describes the components installed by the Helm Chart.

## Loki read and write

By default Loki will be installed in the scalable mode. This consists of a read and write component. These can be scaled-out independently.

## Dashboards

This chart includes dashboards for monitoring Loki. These require the scrape configs defined in the `monitoring.serviceMonitor` and `monitoring.selfMonitoring` sections described below. The dashboards are deployed via a config map which can be mounted on a Grafana instance. The Dashboard require an installation of the Grafana Agent and the Prometheus operator. The agent is installed with this chart.

## Canary

This chart installs the [canary]({{< relref "../../operations/loki-canary" >}}) and its alerts by default. This is another tool to verify the Loki deployment is in a healthy state. It can be disabled with `monitoring.lokiCanary.enabled=false`.

## Gateway

By default and inspired by Grafana's [Tanka setup](https://github.com/grafana/loki/blob/main/production/ksonnet/loki), the chart
installs the gateway component which is an NGINX that exposes Loki's API and automatically proxies requests to the correct
Loki components (read or write, or single instance in the case of filesystem storage).
The gateway must be enabled if an Ingress is required, since the Ingress exposes the gateway only.
If the gateway is enabled, Grafana and log shipping agents, such as Promtail, should be configured to use the gateway.
If NetworkPolicies are enabled, they are more restrictive if the gateway is enabled.

## Caching

By default, this chart configures in-memory caching. If that caching does not work for your deployment, you should setup memcache.
