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

## Recommended Installation

The recommended installation method for initial deployments is to use the [Loki Simple Scalable Helm chart]({{< relref "./install-scalable" >}}). This chart provides a simple scalable deployment mode for Loki, separating execution paths into read, write, and backend targets. For small to medium-sized deployments, this chart is a good starting point.

### Cloud Deployment Guides

The following guides provide step-by-step instructions for deploying Loki on cloud providers:

- [Deploy Loki Simple Scalable Helm chart on AWS]({{< relref "./install-scalable/aws" >}})


## Reference

[Values reference]({{< relref "./reference" >}})
