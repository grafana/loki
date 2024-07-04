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

The loki chart supports three methods of deployment:
- [Monolithic]({{< relref "./install-monolithic" >}}) 
- [Simple Scalable]({{< relref "./install-scalable" >}})
- [Microservice]({{< relref "./install-microservices" >}})

By default, the chart installs the simple scalable method. This is the recommended method for most users. To understand the differences between deployment methods, see the [Loki deployment modes]({{< relref "../../../get-started/deployment-modes/" >}}) documentation.

## Monitoring

The Loki Helm chart does not deploy monitoring by default. Loki clusters can be monitored using the meta monitoring stack, which monitors, the logs, metrics, and traces of the Loki cluster. There are two deployment options for this stack see the installation instructions within [Monitoring]({{< relref "./monitor-and-alert" >}})



## Canary

This chart installs the [canary]({{< relref "../../../operations/loki-canary" >}}) and its alerts by default. This is another tool to verify the Loki deployment is in a healthy state. It can be disabled with `monitoring.lokiCanary.enabled=false`.

## Gateway

By default and inspired by Grafana's [Tanka setup](https://github.com/grafana/loki/blob/main/production/ksonnet/loki), the chart
installs the gateway component which is an NGINX that exposes Loki's API and automatically proxies requests to the correct
Loki components (read or write, or single instance in the case of filesystem storage).
The gateway must be enabled if an Ingress is required, since the Ingress exposes the gateway only.
If the gateway is enabled, Grafana and log shipping agents, such as Promtail, should be configured to use the gateway.
If NetworkPolicies are enabled, they are more restrictive if the gateway is enabled.

## Caching

By default, this chart configures in-memory caching. If that caching does not work for your deployment, you should setup memcache.
