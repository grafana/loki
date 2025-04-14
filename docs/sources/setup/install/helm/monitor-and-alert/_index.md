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

<!-- vale Grafana.We = NO -->
{{< admonition type="warning" >}}
We no longer recommend using the meta-monitoring Helm chart to monitor Loki. To consolidate monitoring efforts into one Helm chart, Grafana Labs recommends using the Kubernetes monitoring Helm chart. Instructions for setting up the Kubernetes monitoring Helm chart can be found under [Manage](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/).
{{< /admonition >}}
<!-- vale Grafana.We = YES -->

There are two common ways to monitor Loki:

- [Monitor using Grafana Cloud (recommended)](with-grafana-cloud/)
- [Monitor using Local Monitoring](with-local-monitoring/)
