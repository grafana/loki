---
title: Install the microservice Helm chart 
menuTitle: Install microservice Loki
description: Installing Loki in microservice (distributed) mode using the Helm chart.
aliases:
  - ../../../installation/helm/microservice/
  - ../../../installation/helm/install-microservice/
weight: 300
keywords: 
---

# Install the microservice Helm chart
<!-- vale Grafana.Quotes = NO -->
<!-- vale Grafana.Quotes = YES -->

This Helm Chart deploys Grafana Loki on Kubernetes.

This chart configures Loki to run Loki in [microservice / distributed mode]({{< relref "../../../../get-started/deployment-modes#microservices-mode" >}}), highly available architecture designed to work with AWS S3 object storage. 

The default Helm chart deploys the following components:
- **Ingester component** (3 replicas): Handles ingestion of data.
- **Querier component** (3 replicas, maxUnavailable: 2): Processes queries. Up to 2 replicas can be unavailable during updates.
- **QueryFrontend component** (2 replicas, maxUnavailable: 1): Manages frontend queries. Up to 1 replica can be unavailable during updates.
- **QueryScheduler component** (2 replicas): Schedules queries.
- **Distributor component** (3 replicas, maxUnavailable: 2): Distributes incoming requests. Up to 2 replicas can be unavailable during updates.
- **Compactor component** (1 replica): Compacts and processes stored data.
- **IndexGateway component** (2 replicas, maxUnavailable: 1): Handles indexing. Up to 1 replica can be unavailable during updates.


<!--TODO - Update when meta-monitoring chart releases-->

It is not recommended to run microservices mode with `filesystem` storage.

**Prerequisites**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster.
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

   - Create the configuration file `values.yaml`. The example below illustrates how to deploy Loki in test mode using MinIO as storage:

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

     To configure other storage providers, refer to the [Helm Chart Reference]({{< relref "../reference" >}}).


1. Install or upgrade the Loki deployment.
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

## AWS S3 Configuration

To configure Loki to use AWS S3 as the object storage, you need to provide the following values in the `values.yaml` file:

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
        accessKeyId: <accessKeyID>
        secretAccessKey: <secretAccessKey>
        s3ForcePathStyle: true
        insecure: true

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

  backend:
    replicas: 0
  read:
    replicas: 0
  write:
    replicas: 0

  singleBinary:
    replicas: 0

```

## Next Steps 
Configure an agent to [send log data to Loki](/docs/loki/<LOKI_VERSION>/send-data/).
