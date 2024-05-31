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

This Helm Chart deploys Grafana Loki on Kubernetes.

This chart configures Loki to run `read`, `write`, and `backend` targets in a [scalable mode]({{< relref "../../../../get-started/deployment-modes#simple-scalable" >}}). Loki’s simple scalable deployment mode separates execution paths into read, write, and backend targets.

The default Helm chart deploys the following components:
- Read component (3 replicas)
- Write component (3 replicas)
- Backend component (3 replicas)
- Loki Canary (1 DaemonSet)
- Gateway (1 NGINX replica)
- Minio (optional, if `minio.enabled=true`)


<!--TODO - Update when meta-monitoring chart releases-->

It is not recommended to run scalable mode with `filesystem` storage. For the purpose of this guide, we will use MinIO as the object storage to provide a complete example. 

**Prerequisites**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster.
- (Optional) A Memcached deployment for better query performance. For information on configuring Memcached, refer to [caching section]({{< relref "../../../../operations/caching" >}}).


**To deploy Loki in simple scalable mode:**


1. Add [Grafana's chart repository](https://github.com/grafana/helm-charts) to Helm:

   ```bash
   helm repo add grafana https://grafana.github.io/helm-charts
   ```

2. Update the chart repository:

   ```bash
   helm repo update
   ```

3. Create the configuration file `values.yaml`. The example below illustrates how to deploy Loki in test mode using MinIO as storage:

     ```yaml
      loki:
        schemaConfig:
          configs:
            - from: 2024-04-01
              store: tsdb
              object_store: s3
              schema: v13
              index:
                prefix: loki_index_
                period: 24h
        ingester:
          chunk_encoding: snappy
        tracing:
          enabled: true
        querier:
          # Default is 4, if you have enough memory and CPU you can increase, reduce if OOMing
          max_concurrent: 4

      #gateway:
      #  ingress:
      #    enabled: true
      #    hosts:
      #      - host: FIXME
      #        paths:
      #          - path: /
      #            pathType: Prefix

      deploymentMode: SimpleScalable

      backend:
        replicas: 3
      read:
        replicas: 3
      write:
        replicas: 3

      # Enable minio for storage
      minio:
        enabled: true

      # Zero out replica counts of other deployment modes
      singleBinary:
        replicas: 0

      ingester:
        replicas: 0
      querier:
        replicas: 0
      queryFrontend:
        replicas: 0
      queryScheduler:
        replicas: 0
      distributor:
        replicas: 0
      compactor:
        replicas: 0
      indexGateway:
        replicas: 0
      bloomCompactor:
        replicas: 0
      bloomGateway:
        replicas: 0
     ```

4. Install or upgrade the Loki deployment.
     - To install:
        ```bash
       helm install --values values.yaml loki grafana/loki
       ```
    - To upgrade:
       ```bash
       helm upgrade --values values.yaml loki grafana/loki
       ```

## AWS S3 Configuration

To configure Loki to use AWS S3 as the object storage rather than MinIO, you need to provide the following values in the `values.yaml` file:

```yaml
    loki:
      schemaConfig:
        configs:
          - from: 2024-04-01
            store: tsdb
            object_store: s3
            schema: v13
            index:
              prefix: loki_index_
              period: 24h
      ingester:
        chunk_encoding: snappy
      tracing:
        enabled: true
      querier:
        max_concurrent: 4

      storage:
        type: s3
        bucketNames:
          chunks: "chunks"
          ruler: "ruler"
          admin: "admin"
        s3:
          s3: <endpoint>
          endpoint: <endpoint>
          accessKeyId: <accessKeyID>
          secretAccessKey: <secretAccessKey>
          s3ForcePathStyle: true
          insecure: true

    deploymentMode: SimpleScalable

    backend:
      replicas: 3
    read:
      replicas: 3
    write:
      replicas: 3

    # Disable minio storage
    minio:
      enabled: false

    # Zero out replica counts of other deployment modes
    singleBinary:
      replicas: 0

    ingester:
      replicas: 0
    querier:
      replicas: 0
    queryFrontend:
      replicas: 0
    queryScheduler:
      replicas: 0
    distributor:
      replicas: 0
    compactor:
      replicas: 0
    indexGateway:
      replicas: 0
    bloomCompactor:
      replicas: 0
    bloomGateway:
      replicas: 0
```

To configure other storage providers, refer to the [Helm Chart Reference]({{< relref "../reference" >}}).

## Next Steps 
* Configure an agent to [send log data to Loki](/docs/loki/<LOKI_VERSION>/send-data/).
* Monitor the Loki deployment using the [Meta Monitoring Healm chart](/docs/loki/<LOKI_VERSION>/monitor-and-alert/)