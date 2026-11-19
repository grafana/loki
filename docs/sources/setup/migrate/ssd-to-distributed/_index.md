---
title: Migrate from SSD to distributed
menuTitle: Migrate from SSD to distributed
description: Migration guide from migrating from a simple scalable deployment to a distributed microservices deployment.
weight: 400
keywords:
  - migrate
  - ssd
  - distributed
  - microservices
---

# Migrate from SSD to distributed

This guide provides instructions for migrating from a [simple scalable deployment (SSD)](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable) to a [distributed microservices deployment](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode) of Loki. Before starting the migration, make sure you have read the [considerations](#considerations) section.

{{< admonition type="caution" >}}
Simple Scalable Deployment (SSD) mode is being deprecated and will be removed with the Loki 4.0 release. You should plan to migrate from SSD to microservices or HA monolithic deployment. You will not be able to run Loki 4.0 in SSD mode.
{{< /admonition >}}

{{< admonition type="note" >}}
In this guide, an AWS deployment is used as an example. However, the migration process is mirrored for other cloud providers. This is due to the fact that no changes are required to the underlying data storage.
{{< /admonition >}}

## Considerations

Migrating from a simple scalable deployment to a distributed deployment requires careful planning. Each `helm upgrade` in this guide restarts pods, so requests can fail for a short time. The following considerations should be taken into account:

1. **Helm Deployment:** This guide assumes that you have deploying Loki using Helm. Other migration methods are possible but are not covered in this guide.
1. **Loki version:** Loki 4.0 removes SSD mode. The `read`, `write`, and `backend` targets don't exist in Loki 4.x, so SSD pods can't run on a 4.x image. Run stage 1 and stage 2 on Loki 3.7 or a later 3.x release and [upgrade to Loki 4.0](#upgrade-to-loki-40) after the migration is complete. Don't set `loki.image.tag` to a 4.x version before then. If you don't set `loki.image.tag`, the Helm chart uses the Loki version of its `appVersion`. Check that this is a 3.x version.
1. **Kubernetes Resources:** This migration method requires you to spin up distributed Loki pods before shutting down the SSD pods. This means that you need to have enough resources in your Kubernetes cluster to run both the SSD and distributed Loki pods at the same time.
1. **Short interruptions:** Both stages restart pods, and stage 2 also switches the gateway routes. Pushes and queries through the gateway can fail with `502` responses or time out during each `helm upgrade`. In a test with a small cluster, requests failed for up to about 30 seconds in stage 1 and up to about 40 seconds in stage 2. The duration depends on your cluster. Make sure that the clients that push logs retry failed requests, and run the migration when traffic is low.
1. **Data:** No changes are required to your underlying data storage. Although data loss or corruption is unlikely, it is always recommended to back up your data before starting the migration process. If you are using a cloud provider you can take a snapshot/backup.
1. **Configuration:** We do not account for all configuration parameters in this guide. We only cover the parameters that need to be changed. Other parameters can remain the same. **However**, if `pattern_ingesters=true` you will need to spin up `patternIngesters` before shutting down the SSD ingesters. This is primarily needed for the Grafana Logs Drilldown feature.
1. **Zone Aware Ingesters:** This guide does not currently account for Zone Aware Ingesters. Our current recommendation is to either disable Zone Aware Ingesters or to consult the [Mimir migration guide](https://grafana.com/docs/helm-charts/mimir-distributed/latest/migration-guides/migrate-from-single-zone-with-helm/). Take note, not all parameters are equivalent between Mimir and Loki.

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
  rulerConfig:
    enable_api: true
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
```

The chart builds the ruler storage configuration from `loki.storage`. Set all other ruler settings, such as `alertmanager_url`, under `loki.rulerConfig`. The chart ignores a `loki.ruler` key.

## Stage 1: Deploying the Loki distributed components

In this stage, we will deploy the distributed Loki components alongside the SSD components. We will also change the `deploymentMode` to `SimpleScalable<->Distributed`. The `SimpleScalable<->Distributed` migration mode allows both deployment types to run at the same time. They share the same object storage backend and the same rings.

Run this stage on Loki 3.x. Don't change `loki.image.tag` to a 4.x version.

The following table outlines which components take over the responsibilities of the SSD components:

Simple Scalable Components |  Distributed Components
---------------------------|--------------------------------
write (Deployment)         |   Distributor + Ingester
read (StatefulSet)         |   Query Frontend + Querier
backend (StatefulSet)      |   Compactor + Ruler + Index Gateway

How Loki handles request routing during the migration:

The Gateway (nginx) handles request routing based on endpoint type:

1. Write Path (`loki/api/v1/push`):
   * In stage 1, routes to the Simple Scalable write component
   * In stage 2, routes to the Distributor. The gateway switches all requests at once, when it restarts with the configuration of stage 2.
   * Both write paths share the same object storage, ensuring data consistency
2. Read Path (`/loki/api/v1/query`):
   * In stage 1, routes to the Simple Scalable read component
   * In stage 2, routes to the distributed Query Frontend
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
     rulerConfig:
       enable_api: true
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
     # Keep the write-ahead log on a persistent volume, like the SSD write pods
     persistence:
       enabled: true
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
   ```

   Here is a breakdown of the changes:
   * `ingester.wal.flush_on_shutdown: true`: This will force the ingesters to flush before shutting down. This is important to prevent data loss.
   * `ingester.persistence.enabled: true`: The SSD write pods keep their write-ahead log on a persistent volume. Without this setting, the distributed ingesters keep it in an `emptyDir` volume, which Kubernetes deletes when it replaces an ingester. Kubernetes doesn't allow adding volume claims to a StatefulSet after it's created, so set this in this stage.
   * `deploymentMode: SimpleScalable<->Distributed`: This will allow for the SSD and distributed components to run simultaneously.
   * Spin up all distributed components with the desired replicas.

1. Deploy the distributed components using the following command:

    ```bash
    helm upgrade --values values-migration.yaml loki grafana-community/loki -n loki 
    ```

   {{< admonition type="caution" >}}
   It is important to allow all components to fully spin up before proceeding to the next stage. You can check the status of the components using the following command:

    ```bash
    kubectl get pods -n loki
    ```

   Let all components reach the `Running` state before proceeding to the next stage.
   {{< /admonition >}}

1. Verify that the distributed components handle requests. In stage 1, the gateway still sends all requests to the SSD components, so the distributed components don't receive requests through it. Before you continue with stage 2, push a log line directly to the distributor and read it back directly from the query frontend.

   Forward the ports of both services. Run each command in a separate terminal:

    ```bash
    kubectl port-forward -n loki svc/loki-distributor 3100:3100
    ```

    ```bash
    kubectl port-forward -n loki svc/loki-query-frontend 3101:3100
    ```

   Push a log line to the distributor. Replace `<TENANT_ID>` with your tenant ID. If you disabled multi-tenancy with `auth_enabled: false`, remove the `X-Scope-OrgID` header. The command prints the status `204`.

    ```bash
    curl -s -w '%{http_code}\n' -X POST http://localhost:3100/loki/api/v1/push \
      -H 'Content-Type: application/json' \
      -H 'X-Scope-OrgID: <TENANT_ID>' \
      -d "{\"streams\":[{\"stream\":{\"job\":\"migration-check\"},\"values\":[[\"$(date +%s)000000000\",\"hello from the distributor\"]]}]}"
    ```

   Query the log line from the query frontend:

    ```bash
    curl -s -G http://localhost:3101/loki/api/v1/query_range \
      -H 'X-Scope-OrgID: <TENANT_ID>' \
      --data-urlencode 'query={job="migration-check"}'
    ```

   The response contains the log line `hello from the distributor`. If the push fails or the response doesn't contain the log line, don't continue with stage 2. Stop both `kubectl port-forward` commands afterwards.

## Stage 2: Transitioning to distributed components

The final stage of the migration involves transitioning all traffic to the distributed components. This is done by scaling down the SSD components and swapping the `deploymentMode` to `Distributed`.

Run this stage on Loki 3.x as well. You upgrade to Loki 4.0 [after](#upgrade-to-loki-40) the deployment runs only distributed components.

To transition all traffic to the distributed components:

1. Create a copy of `values-migration.yaml` and name it `values-distributed.yaml`.

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
     rulerConfig:
       enable_api: true
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
     persistence:
       enabled: true
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
   ```
   
     Here is a breakdown of the changes:
     * `deploymentMode: Distributed`: This will allow for the distributed components to run in isolation.
     * Scale down all SSD components to `0`.

1. Deploy the final configuration using the following command:

    ```bash
    helm upgrade --values values-distributed.yaml loki grafana-community/loki -n loki 
    ```

   {{< admonition type="caution" >}}
   This upgrade removes the SSD components and switches the gateway routes to the distributed components in one step. The previous gateway keeps sending requests to the removed SSD services until the replacement gateway is ready. Pushes and queries fail during this time. Make sure that clients retry failed requests.
   {{< /admonition >}}

1. Once the deployment is complete, you can verify that all components are running using the following command:

    ```bash
    kubectl get pods -n loki
    ```

You should see all distributed components running and the SSD components have now been removed.

## Upgrade to Loki 4.0

After stage 2, the deployment runs only distributed components and you can upgrade to Loki 4.0. Before you upgrade, read the [upgrade notes for Loki 4.0](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/#specific-considerations-before-upgrading-to-loki-40).

The Helm chart renders a `storage_config.boltdb_shipper` block by default. Loki 4.0 removed BoltDB and fails to start with the error `field boltdb_shipper not found in type storage.Config`. To set the image version and remove the block:

1. Create a file named `values-loki-4.yaml`.

    ```yaml
    loki:
      image:
        tag: "4.0.0"
      storage_config:
        # Removes the block that the chart renders by default
        boltdb_shipper: null
    ```

1. Deploy the upgrade. Helm merges the two values files.

    ```bash
    helm upgrade --values values-distributed.yaml --values values-loki-4.yaml loki grafana-community/loki -n loki
    ```

## What's next?

Loki in distributed mode is inherently more complex than SSD mode. It is recommended to meta-monitor your Loki deployment to ensure that everything is running smoothly. You can do this by following the [meta-monitoring guide](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/).

