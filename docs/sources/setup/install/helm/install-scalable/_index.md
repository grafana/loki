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

This Helm Chart deploys Grafana Loki in [simple scalable mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable) within a Kubernetes cluster.

This chart configures Loki to run `read`, `write`, and `backend` targets in a [scalable mode](../../../../get-started/deployment-modes/#simple-scalable). Loki’s simple scalable deployment mode separates execution paths into read, write, and backend targets.

The default Helm chart deploys the following components:
- Read component (3 replicas)
- Write component (3 replicas)
- Backend component (3 replicas)
- Loki Canary (1 DaemonSet)
- Gateway (1 NGINX replica)
- Minio (optional, if `minio.enabled=true`)
- Index and Chunk cache (1 replica)

{{< admonition type="note" >}}
We do not recommended running scalable mode with `filesystem` storage. For the purpose of this guide, we will use MinIO as the object storage to provide a complete example. 
{{< /admonition >}}

## Prerequisites

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster (must have at least 3 nodes).

## Deploying the Helm chart for development and testing

The following steps show how to deploy the Loki Helm chart in simple scalable mode using the included MinIO as the storage backend. Our recommendation is to start here for development and testing purposes. Then configure Loki with an object storage provider when moving to production.


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
            - from: "2024-04-01"
              store: tsdb
              object_store: s3
              schema: v13
              index:
                prefix: loki_index_
                period: 24h
        ingester:
          chunk_encoding: snappy
        querier:
          # Default is 4, if you have enough memory and CPU you can increase, reduce if OOMing
          max_concurrent: 4
        pattern_ingester:
          enabled: true
        limits_config:
          allow_structured_metadata: true
          volume_enabled: true

      deploymentMode: SimpleScalable

      backend:
        replicas: 2
      read:
        replicas: 2
      write:
        replicas: 3 # To ensure data durability with replication

      # Enable minio for storage
      minio:
        enabled: true

      gateway:
        service:
          type: LoadBalancer
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

## Object Storage Configuration

After testing Loki with MinIO, we recommend configuring Loki with an object storage provider. The following examples shows how to configure Loki with different object storage providers:

{{< admonition type="caution" >}}
When deploying Loki using S3 Storage **DO NOT** use the default bucket names;  `chunk`, `ruler` and `admin`. Choose a unique name for each bucket. For more information see the following [security update](https://grafana.com/blog/2024/06/27/grafana-security-update-grafana-loki-and-unintended-data-write-attempts-to-amazon-s3-buckets/). This caution does not apply when you are using MinIO. When using MinIO we recommend using the default bucket names.
{{< /admonition >}}

{{< collapse title="S3" >}}

```yaml
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
      region: <AWS region your bucket is in, for example, `eu-west-2`>
      bucketnames: <Your AWS bucket for chunk, for example, `aws-loki-dev-chunk`>
      s3forcepathstyle: false
  pattern_ingester:
      enabled: true
  limits_config:
    allow_structured_metadata: true
    volume_enabled: true
    retention_period: 672h # 28 days retention
  querier:
    max_concurrent: 4

  storage:
    type: s3
    bucketNames:
        chunks: <Your AWS bucket for chunk, for example, `aws-loki-dev-chunk`>
        ruler: <Your AWS bucket for ruler, for example,  `aws-loki-dev-ruler`>
        admin: <Your AWS bucket for admin, for example,  `aws-loki-dev-admin`>
    s3:
      # s3 URL can be used to specify the endpoint, access key, secret key, and bucket name this works well for S3 compatible storages or if you are hosting Loki on-premises and want to use S3 as the storage backend. Either use the s3 URL or the individual fields below (AWS endpoint, region, secret).
      s3: s3://access_key:secret_access_key@custom_endpoint/bucket_name
      # AWS endpoint URL
      endpoint: <your-endpoint>
      # AWS region where the S3 bucket is located
      region: <your-region>
      # AWS secret access key
      secretAccessKey: <your-secret-access-key>
      # AWS access key ID
      accessKeyId: <your-access-key-id>
      # AWS signature version (e.g., v2 or v4)
      signatureVersion: <your-signature-version>
      # Forces the path style for S3 (true/false)
      s3ForcePathStyle: false
      # Allows insecure (HTTP) connections (true/false)
      insecure: false
      # HTTP configuration settings
      http_config: {}

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
```
  
{{< /collapse >}}
  
{{< collapse title="Azure" >}}
  
```yaml

loki:
  schemaConfig:
    configs:
      - from: "2024-04-01"
        store: tsdb
        object_store: azure
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
    type: azure
    azure:
      # Name of the Azure Blob Storage account
      accountName: <your-account-name>
      # Key associated with the Azure Blob Storage account
      accountKey: <your-account-key>
      # Comprehensive connection string for Azure Blob Storage account (Can be used to replace endpoint, accountName, and accountKey)
      connectionString: <your-connection-string>
      # Flag indicating whether to use Azure Managed Identity for authentication
      useManagedIdentity: false
      # Flag indicating whether to use a federated token for authentication
      useFederatedToken: false
      # Client ID of the user-assigned managed identity (if applicable)
      userAssignedId: <your-user-assigned-id>
      # Timeout duration for requests made to the Azure Blob Storage account (in seconds)
      requestTimeout: <your-request-timeout>
      # Domain suffix of the Azure Blob Storage service endpoint (e.g., core.windows.net)
      endpointSuffix: <your-endpoint-suffix>
    bucketNames:
      chunks: "chunks"
      ruler: "ruler"
      admin: "admin"

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


``` 
{{< /collapse >}}

To configure other storage providers, refer to the [Helm Chart Reference](../reference/).

## Next Steps 
* Configure an agent to [send log data to Loki](/docs/loki/<LOKI_VERSION>/send-data/).
* Monitor the Loki deployment using the [Meta Monitoring Helm chart](/docs/loki/<LOKI_VERSION>/setup/install/helm/monitor-and-alert/)
