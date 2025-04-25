---
title: Migrate from SSD to distributed
menuTitle: Migrate from SSD to distributed
description: Migration guide from migrating from simple scalable deployment to a distributed microservices deployment.
weight: 300
keywords:
  - migrate
  - distributed
  - ssd
---

# Migrate from SSD to distributed

This guide provides instructions for migrating from a [simple scalable deployment (SSD)](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable) to a [distributed microservices deployment](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode) of Loki. Before starting the migration, make sure you have read the [considerations](#considerations) section. 

{{< admonition type="note" >}}
In this guide, an AWS deployment is used as an example. However, the migration process is mirrored for other cloud providers. This is due to the fact that no changes are required to the underlying data storage.
{{< /admonition >}}

## Considerations

Migrating from a simple scalable deployment to a distributed deployment with zero downtime is possible but requires careful planning. The following considerations should be taken into account:

1. **Helm Deployment:** This guide assumes that you have deploying Loki using Helm. Other migration methods are possible but are not covered in this guide.
1. **Kubernetes Resources:** This migration method requires you to spin up distributed Loki pods before shutting down the SSD pods. This means that you need to have enough resources in your Kubernetes cluster to run both the SSD and distributed Loki pods at the same time.
1. **Data:** No changes are required to your underlying data storage. Although data loss or corruption is unlikely, it is always recommended to back up your data before starting the migration process. If you are using a cloud provider you can take a snapshot/backup.
1. **Configuration:** We do not account for all configuration parameters in this guide. We only cover the parameters that need to be changed. Other parameters can remain the same. **However**, if `pattern_ingesters=true` you will need to spin up `patternIngesters` before shutting down the SSD ingesters. This is primarily needed for the Grafana drilldown logs feature.
1. **Zone Aware Ingesters:** This guide does not currently account for Zone Aware Ingesters. Our current recommendation is to either disable Zone Aware Ingesters or to consult the [Mimir migration guide](https://grafana.com/docs/helm-charts/mimir-distributed/latest/migration-guides/migrate-from-single-zone-with-helm/). Take note not all paramters are equivalent between Mimir and Loki. 

## Prerequisites

Before starting the migration process, make sure you have the following prerequisites:

1. Access to your Kubernetes cluster via `kubectl`.
1. Helm installed.

## Example SSD deployment

This example will use the following SSD deployment as a reference:

{{< admonition type="note" >}}
This example is only a reference on the parameters that need to be changed. There will be other parameters within your own config such as `limits_config`, `gateway`, `compactor`, etc. These can remain the same.
{{< /admonition >}}

```yaml
---
loki:
   schemaConfig:
     configs:
       - from: "2024-04-01"
         store: tsdb
         object_store: s3
         schema: v13
         index:
           prefix: loki_index_
           period: 24h
   storage_config:
     aws:
       region: eu-central-1
       bucketnames: aws-chunks-bucket
       s3forcepathstyle: false
   ingester:
       chunk_encoding: snappy
   ruler:
    enable_api: true
    storage:
      type: s3
      s3:
        region: eu-central-1
        bucketnames: aws-ruler-bucket
        s3forcepathstyle: false
      alertmanager_url: http://prom:9093
   querier:
      max_concurrent: 4

   storage:
      type: s3
      bucketNames:
        chunks: "aws-chunks-bucket"
        ruler: "aws-ruler-bucket"
      s3:
        region: eu-central-1

deploymentMode: SimpleScalable

# SSD
backend:
 replicas: 2
read:
 replicas: 3
write:
 replicas: 3

# Distributed Loki
ingester:
 replicas: 0
 zoneAwareReplication:
  enabled: false

querier:
 replicas: 0
 maxUnavailable: 0
queryFrontend:
 replicas: 0
 maxUnavailable: 0
queryScheduler:
 replicas: 0
distributor:
 replicas: 0
 maxUnavailable: 0
compactor:
 replicas: 0
indexGateway:
 replicas: 0
 maxUnavailable: 0
ruler:
 replicas: 0
 maxUnavailable: 0

# Single binary Loki
singleBinary:
 replicas: 0

minio:
 enabled: false
```

## Stage 1: Deploying the Loki distributed components

In this stage, we will deploy the distributed Loki components alongside the SSD components. We will also change the `deploymentMode` to `SimpleScalable<->Distributed`. The `SimpleScalable<->Distributed` migration mode allows for a zero-downtime transition between Simple Scalable and fully Distributed architectures. During migration, both deployment types run simultaneously, sharing the same object storage backend.

The the following table outlines which components take over the responsibilities of the SSD components:

Simple Scalable Components |  Distributed Components
---------------------------|--------------------------------
write (Deployment)         |   Distributor + Ingester
read (StatefulSet)         |   Query Frontend + Querier
backend (StatefulSet)      |   Compactor + Ruler + Index Gateway

How Loki handles request routing during the migration:

The Gateway (nginx) handles request routing based on endpoint type:
1. Write Path (`loki/api/v1/push`):
   * Initially routes to Simple Scalable write component
   * Gradually shifted to the Distributor
   * Both write paths share the same object storage, ensuring data consistency
2. Read Path (`/loki/api/v1/query`):
   * Routes to either Simple Scalable read or Distributed Query Frontend
   * Query results are consistent since both architectures read from same storage
3. Admin/Background Operations:
   * Compaction, retention, and rule evaluation handled by either backend or respective distributed components
   * Operations are coordinated through object storage locks

To start the migration process:  

1. Create a copy of your existing `values.yaml` file and name it `values-migration.yaml`.

    ```bash
    cp values.yaml values-migration.yaml
    ```

1. Next modify the following parameters; `deploymentMode`, `ingester` and components based on the annotations below.

   ```yaml
   ---
   loki:
     schemaConfig:
       configs:
         - from: "2024-04-01"
           store: tsdb
           object_store: s3
           schema: v13
           index:
             prefix: loki_index_
             period: 24h
     storage_config:
       aws:
         region: eu-central-1
         bucketnames: aws-chunks-bucket
         s3forcepathstyle: false
     ingester:
       chunk_encoding: snappy
       # Add this to ingester; this will force ingesters to flush before shutting down
       wal:
         flush_on_shutdown: true
     ruler:
       enable_api: true
       storage:
         type: s3
         s3:
           region: eu-central-1
           bucketnames: aws-ruler-bucket
           s3forcepathstyle: false
         alertmanager_url: http://prom:9093
     querier:
       max_concurrent: 4

     storage:
       type: s3
       bucketNames:
         chunks: "aws-chunks-bucket"
         ruler: "aws-ruler-bucket"
       s3:
         region: eu-central-1

   # Important: Make sure to change this to SimpleScalable<->Distributed
   deploymentMode: SimpleScalable<->Distributed

   # SSD
   backend:
     replicas: 2
   read:
     replicas: 3
   write:
     replicas: 3

   # Distributed Loki
   # Spin up the distributed components
   ingester:
     replicas: 3
     zoneAwareReplication:
       enabled: false
   querier:
     replicas: 3
     maxUnavailable: 0
   queryFrontend:
     replicas: 2
     maxUnavailable: 0
   queryScheduler:
     replicas: 2
   distributor:
     replicas: 2
     maxUnavailable: 0
   compactor:
     replicas: 1
   indexGateway:
     replicas: 2
     maxUnavailable: 0
   ruler:
     replicas: 1
     maxUnavailable: 0

   # Single binary Loki
   singleBinary:
     replicas: 0

   minio:
     enabled: false
   ```

   Here is a breakdown of the changes:
   * `ingester.wal.flush_on_shutdown: true`: This will force the ingesters to flush before shutting down. This is important to prevent data loss.
   * `deploymentMode: SimpleScalable<->Distributed`: This will allow for the SSD and distributed components to run simultaneously.
   * Spin up all distributed components with the desired replicas.

1. Deploy the distributed components using the following command:

    ```bash
    helm upgrade --values values-migration.yaml loki grafana/loki -n loki 
    ```
   {{< admonition type="caution" >}}
   It is important to allow all components to fully spin up before proceeding to the next stage. You can check the status of the components using the following command:

    ```bash
    kubectl get pods -n loki
    ```
   Let all components reach the `Running` state before proceeding to the next stage.
   {{< /admonition >}}

## Stage 2: Transitioning to distributed components

The final stage of the migration involves transitioning all traffic to the distributed components. This is done by scaling down the SSD components and swapping the `deploymentMode` to `Distributed`. To do this:

1. Create a copy of `values-migration.yaml` and name it `values-distributed.yaml` (Once again this can all be done in the original file).

    ```bash
    cp values-migration.yaml values-distributed.yaml
    ```
1. Next modify the following parameters; `deploymentMode` and components based on the annotations below.
   ```yaml
   ---
   loki:
     schemaConfig:
       configs:
         - from: "2024-04-01"
           store: tsdb
           object_store: s3
           schema: v13
           index:
             prefix: loki_index_
             period: 24h
     storage_config:
       aws:
         region: eu-central-1
         bucketnames: aws-chunks-bucket
         s3forcepathstyle: false
     ingester:
       chunk_encoding: snappy
       wal:
         flush_on_shutdown: true
     ruler:
       enable_api: true
       storage:
         type: s3
         s3:
           region: eu-central-1
           bucketnames: aws-ruler-bucket
           s3forcepathstyle: false
         alertmanager_url: http://prom:9093
     querier:
       max_concurrent: 4

     storage:
       type: s3
       bucketNames:
         chunks: "aws-chunks-bucket"
         ruler: "aws-ruler-bucket"
       s3:
         region: eu-central-1

   # Important: Make sure to change this to Distributed
   deploymentMode: Distributed

   # SSD
   # Scale down the SSD components
   backend:
     replicas: 0
   read:
     replicas: 0
   write:
     replicas: 0

   # Distributed Loki
   ingester:
     replicas: 3
     zoneAwareReplication:
       enabled: false
   querier:
     replicas: 3
     maxUnavailable: 0
   queryFrontend:
     replicas: 2
     maxUnavailable: 0
   queryScheduler:
     replicas: 2
   distributor:
     replicas: 2
     maxUnavailable: 0
   compactor:
     replicas: 1
   indexGateway:
     replicas: 2
     maxUnavailable: 0
   ruler:
     replicas: 1
     maxUnavailable: 0

   # Single binary Loki
   singleBinary:
     replicas: 0

   minio:
     enabled: false
   ```
  Here is a breakdown of the changes:
  * `deploymentMode: Distributed`: This will allow for the distributed components to run in isolation.
  * Scale down all SSD components to `0`.

1. Deploy the final configuration using the following command:

    ```bash
    helm upgrade --values values-distributed.yaml loki grafana/loki -n loki 
    ```

1. Once the deployment is complete, you can verify that all components are running using the following command:

    ```bash
    kubectl get pods -n loki
    ```

You should see all distributed components running and the SSD compontents have now been removed.

## What's next?

Loki in distributed mode is inherently more complex than SSD mode. It is recommended to meta-minitor your Loki deployment to ensure that everything is running smoothly. You can do this by following the [meta-monitoring guide](https://grafana.com/docs/loki/latest/operations/meta-monitoring/). 

