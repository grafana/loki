---
title: Monitoring
description: Provides links to the two common ways to monitor Loki.
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

{{< admonition type="warning" >}}
We no longer recommend using the meta-monitoring Helm chart to monitor Loki. To consolidate monitoring efforts into one Helm chart, we recommend using the Kubernetes monitoring Helm chart. Instructions for setting up the Kubernetes monitoring Helm chart can be found in [operations](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/).
{{< /admonition >}}


There are two common ways to monitor Loki:

- [Monitor using Grafana Cloud (recommended)](with-grafana-cloud/)
- [Monitor using Local Monitoring](with-local-monitoring/)
