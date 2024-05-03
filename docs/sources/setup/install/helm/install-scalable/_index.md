---
title: Install the simple scalable Helm chart 
menuTitle: Install scalable Loki
description: Installing Loki in simple scalable mode using the Helm chart.
aliases:
  - ../../../installation/helm/scalable/
  - ../../../installation/helm/install-scalable/
weight: 300
keywords: 
---

# Install the simple scalable Helm chart
<!-- vale Grafana.Quotes = NO -->
<!-- vale Grafana.Quotes = YES -->

This Helm Chart deploys Grafana Loki on Kubernetes.

This chart configures Loki to run `read`, `write`, and `backend` targets in a [scalable mode]({{< relref "../../../../get-started/deployment-modes#simple-scalable" >}}), highly available architecture designed to work with AWS S3 object storage. The chart also supports self-monitoring or meta-monitoring by deploying Grafana Agent to monitor Loki itself, by scraping its metrics and logs. 

The default Helm chart deploys the following components:
- Read component (3 replicas)
- Write component (3 replicas)
- Backend component (3 replicas)
- Loki Canary (1 DaemonSet)
- Gateway (1 NGINX replica)
- Minio (optional, if `minio.enabled=true`)
- Grafana Agent Operator + Grafana Agent (1 DaemonSet) - configured to monitor the Loki application.

<!--TODO - Update when meta-monitoring chart releases-->

It is not recommended to run scalable mode with `filesystem` storage.

**Prerequisites**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster (must have at least 3 nodes).
- (Optional) A Memcached deployment for better query performance. For information on configuring Memcached, refer to [caching section]({{< relref "../../../../operations/caching" >}}).


**To deploy Loki in simple scalable mode:**


1. Add [Grafana's chart repository](https://github.com/grafana/helm-charts) to Helm:

   ```bash
   helm repo add grafana https://grafana.github.io/helm-charts
   ```

1. Update the chart repository:

   ```bash
   helm repo update
   ```

1. Configure the object storage:

   - Create the configuration file `values.yaml`. The example below illustrates a s3 configuration:

     ```yaml
     loki:
       storage:
         bucketNames:
           chunks: chunks
           ruler: ruler
           admin: admin
         type: s3
         s3:
           endpoint: <endpoint>
           region: <AWS region>
           secretAccessKey: <AWS secret access key>
           accessKeyId: <AWS access key ID>
           s3ForcePathStyle: false
           insecure: false
     ```

     To configure other storage providers, refer to the [Helm Chart Reference]({{< relref "../reference" >}}).

   - If you're just trying things, you can use the following configuration, that sets MinIO as storage:
     ```yaml
     minio:
       enabled: true
     ```

1. Install or upgrade the Loki deployment.
     - To install:
        ```bash
       helm install --values values.yaml loki grafana/loki
       ```
    - To upgrade:
       ```bash
       helm upgrade --values values.yaml loki grafana/loki
       ```

## Next Steps 
Configure an agent to [send log data to Loki](/docs/loki/<LOKI_VERSION>/send-data/).
