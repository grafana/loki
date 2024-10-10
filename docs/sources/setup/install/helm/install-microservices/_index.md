---
title: Install the microservice Helm chart 
menuTitle: Install microservice Loki
description: Installing Loki in microservice (distributed) mode using the Helm chart.
weight: 300
keywords: 
---

# Install the microservice Helm chart

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

## Object Storage Configuration

After testing Loki with MinIO, it is recommended to configure Loki with an object storage provider. The following examples shows how to configure Loki with different object storage providers:

{{< admonition type="caution" >}}
When deploying Loki using S3 Storage **DO NOT** use the default bucket names;  `chunk`, `ruler` and `admin`. Choose a unique name for each bucket. For more information see the following [security update](https://grafana.com/blog/2024/06/27/grafana-security-update-grafana-loki-and-unintended-data-write-attempts-to-amazon-s3-buckets/). This caution does not apply when you are using MinIO. When using MinIO we recommend using the default bucket names.
{{< /admonition >}}

{{< code >}}

```s3
# Example configuration for Loki with S3 storage

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
        chunks: "<INSERT BUCKET NAME>"
        ruler: "<INSERT BUCKET NAME>"
        admin: "<INSERT BUCKET NAME>"
      s3:
        # s3 URL can be used to specify the endpoint, access key, secret key, and bucket name
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

```azure
# Example configuration for Loki with Azure Blob Storage

loki:
  schemaConfig:
    configs:
      - from: 2024-04-01
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
{{< /code >}}

To configure other storage providers, refer to the [Helm Chart Reference]({{< relref "../reference" >}}).

## Next Steps 
* Configure an agent to [send log data to Loki](/docs/loki/<LOKI_VERSION>/send-data/).
* Monitor the Loki deployment using the [Meta Monitoring Helm chart](/docs/loki/<LOKI_VERSION>/setup/install/helm/monitor-and-alert/)
