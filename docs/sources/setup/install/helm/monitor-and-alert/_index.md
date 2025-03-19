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
We no longer recommend using the meta monitoring helm chart to monitor Loki. To consolidate monitoring efforts into one helm chart, we recommend using the Kubernetes monitoring helm chart. Instructions for setting up the Kubernetes monitoring helm chart can be found in [operations](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/).
{{< /admonition >}}


There are two common ways to monitor Loki:

- [Monitor using Grafana Cloud (recommended)]({{< relref "./with-grafana-cloud" >}})
- [Monitor using Local Monitoring]({{< relref "./with-local-monitoring" >}})
