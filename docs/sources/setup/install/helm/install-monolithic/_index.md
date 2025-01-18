---
title: Install the monolithic Helm chart
menuTitle: Install monolithic Loki
description: Installing Loki in monolithic, single binary mode using the Helm chart.
aliases:
 - ../../../installation/helm/monolithic
 - ../../../installation/helm/install-monolithic/
weight: 100
---

# Install the monolithic Helm chart

This Helm Chart installation deploys Grafana Loki in [monolithic mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#monolithic-mode) within a Kubernetes cluster.

## Prerequisites

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster.

## Single Replica or Multiple Replicas

There are two ways to deploy Loki in monolithic mode:
1. **Single Replica**: Run Loki with a single replica. This mode is useful for testing and development or if you are planning to run Loki as a meta-monitoring system.
2. **Multiple Replicas**: Run Loki with multiple replicas. This mode is useful for high availability. This mode is less economical than microservice mode, but it is simpler to operate. We recommend running at least three replicas for high availability.

Once you have selected how many replicas you would like to deploy, choose the appropriate `values.yaml` configuration file below and then continue with the deployment steps.
  
### Single Replica

Deploying the Helm chart with a single replica deploys the following components:
- Loki (1 replica)
- Loki Canary (1 DaemonSet)
- Loki Gateway (1 NGINX replica)
- Loki Chunk and Result Cache (1 DaemonSet)
- Minio (optional, if `minio.enabled=true`)

Create the configuration file `values.yaml`:

{{< admonition type="note" >}}
You must specify `commonConfig.replication_factor: 1` if you are only using 1 replica, otherwise requests will fail.
{{< /admonition >}}

```yaml
loki:
  commonConfig:
    replication_factor: 1
  schemaConfig:
    configs:
      - from: "2024-04-01"
        store: tsdb
        object_store: s3
        schema: v13
        index:
          prefix: loki_index_
          period: 24h
  pattern_ingester:
      enabled: true
  limits_config:
    allow_structured_metadata: true
    volume_enabled: true
  ruler:
    enable_api: true

minio:
  enabled: true
      
deploymentMode: SingleBinary

singleBinary:
  replicas: 1

# Zero out replica counts of other deployment modes
backend:
  replicas: 0
read:
  replicas: 0
write:
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

In this configuration, we are deploying Loki with MinIO as the object storage. We recommend configuring object storage via cloud provider or pointing Loki at a MinIO cluster for production deployments.

### Multiple Replicas  

Deploying the Helm chart with multiple replicas deploys the following components:
- Loki (3 replicas)
- Loki Canary (1 DaemonSet)
- Loki Gateway (1 NGINX replica)
- Loki Chunk and Result Cache (1 DaemonSet)
- Minio (optional, if `minio.enabled=true`)

Create the configuration file `values.yaml`:

{{< admonition type="note" >}}
If you set the `singleBinary.replicas` value to 2 or more, this chart configures Loki to run a *single binary* in a replicated, highly available mode. When running replicas of a single binary, you must configure object storage.
{{< /admonition >}}

```yaml
loki:
  commonConfig:
    replication_factor: 3
  schemaConfig:
    configs:
      - from: "2024-04-01"
        store: tsdb
        object_store: s3
        schema: v13
        index:
          prefix: loki_index_
          period: 24h
  pattern_ingester:
      enabled: true
  limits_config:
    allow_structured_metadata: true
    volume_enabled: true
  ruler:
    enable_api: true

minio:
  enabled: true
      
deploymentMode: SingleBinary

singleBinary:
  replicas: 3

# Zero out replica counts of other deployment modes
backend:
  replicas: 0
read:
  replicas: 0
write:
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
In this configuration, we need to make sure to update the `commonConfig.replication_factor` and `singleBinary.replicas` to the desired number of replicas. We are deploying Loki with MinIO as the object storage. We recommend configuring object storage via cloud provider or pointing Loki at a MinIO cluster for production deployments.

## Deploying the Helm chart for development and testing

1. Add [Grafana's chart repository](https://github.com/grafana/helm-charts) to Helm:

   ```bash
   helm repo add grafana https://grafana.github.io/helm-charts
   ```

1. Update the chart repository:

   ```bash
   helm repo update
   ```

1. Deploy Loki using the configuration file `values.yaml`:

   ```bash
    helm install loki grafana/loki -f values.yaml
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
       
1. Verify that Loki is running:
    ```bash
    kubectl get pods -n loki
    ```

## Object Storage Configuration

After testing Loki with MinIO, we recommend configuring Loki with an object storage provider. The following examples shows how to configure Loki with different object storage providers:

{{< admonition type="caution" >}}
When deploying Loki using S3 Storage **DO NOT** use the default bucket names;  `chunk`, `ruler` and `admin`. Choose a unique name for each bucket. For more information see the following [security update](https://grafana.com/blog/2024/06/27/grafana-security-update-grafana-loki-and-unintended-data-write-attempts-to-amazon-s3-buckets/). This caution does not apply when you are using MinIO. When using MinIO we recommend using the default bucket names.
{{< /admonition >}}

{{< collapse title="S3" >}}

```yaml
loki:
  commonConfig:
    replication_factor: 3
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
      bucketnames: <Your AWS bucket for chunk, for exaxmple,  `aws-loki-dev-chunk`>
      s3forcepathstyle: false
  pattern_ingester:
      enabled: true
  limits_config:
    allow_structured_metadata: true
    volume_enabled: true
    retention_period: 672h # 28 days retention

  storage:
    type: s3
    bucketNames:
        chunks: <Your AWS bucket for chunk, for example, `aws-loki-dev-chunk`>
        ruler: <Your AWS bucket for ruler, for example, `aws-loki-dev-ruler`>
        admin: <Your AWS bucket for admin, for example, `aws-loki-dev-admin`>
    s3:
      # s3 URL can be used to specify the endpoint, access key, secret key, and bucket name this works well for S3 compatible storages or are hosting Loki on-premises and want to use S3 as the storage backend. Either use the s3 URL or the individual fields below (AWS endpoint, region, secret).
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

# Disable minio storage
minio:
  enabled: false

deploymentMode: SingleBinary

singleBinary:
  replicas: 3
  persistence:
    storageClass: gp2
    accessModes:
      - ReadWriteOnce
    size: 30Gi

# Zero out replica counts of other deployment modes
backend:
  replicas: 0
read:
  replicas: 0
write:
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

# Disable minio storage
minio:
  enabled: false

deploymentMode: SingleBinary

singleBinary:
  replicas: 3
  persistence:
    storageClass: gp2
    accessModes:
      - ReadWriteOnce
    size: 30Gi

# Zero out replica counts of other deployment modes
backend:
  replicas: 0
read:
  replicas: 0
write:
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
  
{{< /collapse >}}
  


To configure other storage providers, refer to the [Helm Chart Reference](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/reference/).

## Deploying the Loki Helm chart to a Production Environment

{{< admonition type="note" >}}
We are actively working on providing more guides for deploying Loki in production. 
{{< /admonition >}}

We recommend running Loki at scale within a cloud environment like AWS, Azure, or GCP. The below guides will show you how to deploy a minimally viable production environment.
- [Deploy Loki on AWS](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/deployment-guides/aws)


## Next Steps 
* Configure an agent to [send log data to Loki](/docs/loki/<LOKI_VERSION>/send-data/).
* Monitor the Loki deployment using the [Meta Monitoring Helm chart](/docs/loki/<LOKI_VERSION>/setup/install/helm/monitor-and-alert/)

