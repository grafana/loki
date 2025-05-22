---
title: Install Grafana Loki with Helm
menuTitle: Install using Helm
description: Overview of topics for how to install Grafana Loki on Kubernetes with Helm.
aliases:
  - ../../installation/helm/
weight: 200
keywords:
  - helm 
  - scalable
  - simple-scalable
  - installation
---

# Install Grafana Loki with Helm

The [Helm](https://helm.sh/) chart lets you configure, install, and upgrade Grafana Loki within a Kubernetes cluster.

This guide references the Loki Helm chart version 3.0 or greater and contains the following sections:

{{< section menuTitle="true" >}}

If you are installing Grafana Enterprise Logs, follow the [GEL Helm installation](https://grafana.com/docs/enterprise-logs/<ENTERPRISE_LOGS_VERSION>/setup/helm/).

## Deployment Recommendations

Loki is designed to be run in two states:
* [Monolithic](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#monolithic-mode): Recommended when you are running Loki as part of a small meta monitoring stack.
* [Microservices](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode): For workloads that require high availability and scalability. Loki is deployed in this mode internally at Grafana Labs.

{{< admonition type="tip" >}}
Loki can also be deployed in [Simple Scalable mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable). For the best possible experience in production, we recommend deploying Loki in *microservices* mode.
{{< /admonition >}}

## Cloud Deployment Guides

The following guides provide step-by-step instructions for deploying Loki on cloud providers:

- [Amazon EKS](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/deployment-guides/aws/)
- [Azure AKS](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/deployment-guides/azure/)

## Reference

[Values reference](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/reference/)
