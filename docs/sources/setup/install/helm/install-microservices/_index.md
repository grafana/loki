---
title: Install the microservice Helm chart 
menuTitle: Install microservice Loki
description: Installing Loki in microservice (distributed) mode using the Helm chart.
aliases:
weight: 300
keywords: 
---

# Install the microservice Helm charts

This Helm Chart deploys Grafana Loki on Kubernetes.

This chart configures Loki to run Loki in [microservice / distributed mode]({{< relref "../../../../get-started/deployment-modes#microservices-mode" >}}). The microservices deployment mode runs components of Loki as distinct processes.

The default Helm chart deploys the following components:
- **Compactor component** (1 replica): Compacts and processes stored data.
- **Distributor component** (3 replicas, maxUnavailable: 2): Distributes incoming requests. Up to 2 replicas can be unavailable during updates.
- **IndexGateway component** (2 replicas, maxUnavailable: 1): Handles indexing. Up to 1 replica can be unavailable during updates.
- **Ingester component** (3 replicas): Handles ingestion of data.
- **Querier component** (3 replicas, maxUnavailable: 2): Processes queries. Up to 2 replicas can be unavailable during updates.
- **QueryFrontend component** (2 replicas, maxUnavailable: 1): Manages frontend queries. Up to 1 replica can be unavailable during updates.
- **QueryScheduler component** (2 replicas): Schedules queries.

It is not recommended to run scalable mode with `filesystem` storage. For the purpose of this guide, we will use MinIO as the object storage to provide a complete example. 

**Prerequisites**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster.
- (Optional) A Memcached deployment for better query performance. For information on configuring Memcached, refer to the [caching section](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/caching/).


**To deploy Loki in microservice mode (with MinIO):**


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

        deploymentMode: Distributed

        ingester:
          replicas: 3
        querier:
          replicas: 3
          maxUnavailable: 2
        queryFrontend:
          replicas: 2
          maxUnavailable: 1
        queryScheduler:
          replicas: 2
        distributor:
          replicas: 3
          maxUnavailable: 2
        compactor:
          replicas: 1
        indexGateway:
          replicas: 2
          maxUnavailable: 1

        bloomCompactor:
          replicas: 0
        bloomGateway:
          replicas: 0

        # Enable minio for storage
        minio:
          enabled: true

        # Zero out replica counts of other deployment modes
        backend:
          replicas: 0
        read:
          replicas: 0
        write:
          replicas: 0

        singleBinary:
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
       

1. Verify that Loki is running:
    ```bash
    kubectl get pods -n loki
    ```
    The output should an output similar to the following:

    ```bash
      loki-canary-8thrx                      1/1     Running   0          167m
      loki-canary-h965l                      1/1     Running   0          167m
      loki-canary-th8kb                      1/1     Running   0          167m
      loki-chunks-cache-0                    0/2     Pending   0          167m
      loki-compactor-0                       1/1     Running   0          167m
      loki-compactor-1                       1/1     Running   0          167m
      loki-distributor-7c9bb8f4dd-bcwc5      1/1     Running   0          167m
      loki-distributor-7c9bb8f4dd-jh9h8      1/1     Running   0          167m
      loki-distributor-7c9bb8f4dd-np5dw      1/1     Running   0          167m
      loki-gateway-77bc447887-qgc56          1/1     Running   0          167m
      loki-index-gateway-0                   1/1     Running   0          167m
      loki-index-gateway-1                   1/1     Running   0          166m
      loki-ingester-zone-a-0                 1/1     Running   0          167m
      loki-ingester-zone-b-0                 1/1     Running   0          167m
      loki-ingester-zone-c-0                 1/1     Running   0          167m
      loki-minio-0                           1/1     Running   0          167m
      loki-querier-bb8695c6d-bv9x2           1/1     Running   0          167m
      loki-querier-bb8695c6d-bz2rw           1/1     Running   0          167m
      loki-querier-bb8695c6d-z9qf8           1/1     Running   0          167m
      loki-query-frontend-6659566b49-528j5   1/1     Running   0          167m
      loki-query-frontend-6659566b49-84jtx   1/1     Running   0          167m
      loki-query-frontend-6659566b49-9wfr7   1/1     Running   0          167m
      loki-query-scheduler-f6dc4b949-fknfk   1/1     Running   0          167m
      loki-query-scheduler-f6dc4b949-h4nwh   1/1     Running   0          167m
      loki-query-scheduler-f6dc4b949-scfwp   1/1     Running   0          167m
      loki-results-cache-0                   2/2     Running   0          167m
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

  deploymentMode: Distributed

  # Disable minio storage
  minio:
      enabled: false

  ingester:
    replicas: 3
  querier:
    replicas: 3
    maxUnavailable: 2
  queryFrontend:
    replicas: 2
    maxUnavailable: 1
  queryScheduler:
    replicas: 2
  distributor:
    replicas: 3
    maxUnavailable: 2
  compactor:
    replicas: 1
  indexGateway:
    replicas: 2
    maxUnavailable: 1

  bloomCompactor:
    replicas: 0
  bloomGateway:
    replicas: 0

  backend:
    replicas: 0
  read:
    replicas: 0
  write:
    replicas: 0

  singleBinary:
    replicas: 0

```

To configure other storage providers, refer to the [Helm Chart Reference]({{< relref "../reference" >}}).

## Next Steps 
* Configure an agent to [send log data to Loki](/docs/loki/<LOKI_VERSION>/send-data/).
* Monitor the Loki deployment using the [Meta Monitoring Healm chart](/docs/loki/<LOKI_VERSION>/monitor-and-alert/)
