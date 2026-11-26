---
title: Horizontal scaling of Compactor
menuTitle: Horizontal scaling of Compactor
description: Describes how to run the Loki compactor in horizontally scalable mode and the available configuration options.
weight: 800
---

# Horizontal scaling of Compactor

{{< admonition type="caution" >}}
Compactor horizontal scaling is an experimental feature. Use it with caution in your production environments.
{{< /admonition >}}

Grafana Loki saw a major improvement in its operational complexity and cost-effectiveness with the introduction of object-storage-based indexes.
This change also led to the addition of a singleton Compactor service, initially responsible only for index compaction.
However, as new features like [Custom Retention](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/retention/) and [Deletion of Logs with line filters](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/logs-deletion/) were introduced, the Compactor's responsibilities grew.
With increasing scale and more demanding features, especially log deletion with line filters, the singleton Compactor began to show its scaling limits.

Now, you can run the Loki Compactor in a horizontally scalable mode.
Because log deletion with line filters is the compactor's most operationally intensive work, horizontal scaling focuses on distributing that workload across multiple instances.

## Prerequisites

Before you enable horizontal scaling, confirm that your deployment meets the following requirements:

- `compactor.retention_enabled` must be set to `true`. The Main compactor only builds deletion jobs, and a Worker only registers a job runner, when retention is enabled. If retention is disabled, a Worker instance fails to start with the error `no job runners registered`.
- `compactor.delete_request_store` must be configured, because it is required whenever `retention_enabled` is `true`, regardless of the horizontal scaling mode. Refer to [Log entry deletion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/logs-deletion/) for details on configuring deletion.
- The active index schema must use the TSDB index type. A Worker fails to start otherwise.

## How compactor horizontal scaling works

The Compactor supports two modes for operating in horizontally scalable mode:

1. Main:

   * Runs all Compactor functions and distributes chunk processing for log line deletion with filters to the workers.
   * Should be deployed as a singleton with access to a disk, like the current singleton deployment.
2. Worker:

   * Connects to the Main Compactor over gRPC to get and execute the jobs.
   * Multiple replicas can be deployed to achieve higher job processing throughput.
   * Only needs access to object storage for reading/writing chunks.

### Compactor horizontal scaling implementation details

Horizontally scalable Compactor currently only supports distributing the work of log line deletion with filters. Future releases might add support for distributing other kinds of work to the Workers.
The rest of this section describes the implementation details of the current functionality.

#### Main mode

For distribution of chunk processing work from the Main compactor, the following core components are involved:

1. **Deletion Manifest Builder**: The manifest builder works on a batch of delete requests to discover all the chunks covered by them based on their label filters and time range.
Using the discovered chunks, it creates structured manifests and stores them in object storage. A deletion manifest contains:

   - *Segments*: Groups of up to 100K chunks per segment. Also acts as partition for chunks by Loki Tenant/Table.
   - *Manifest*: Complete metadata about all segments and requests to process.

2. **Job Builder**: The job builder converts manifests into discrete jobs and manages their lifecycle:

   - *Job Creation*: Breaks segments into jobs of up to 1K chunks each. Also includes line filters to apply on the chunks to remove the log lines requested for deletion.
   - *Progress Tracking*: Monitors job completion and stops processing of manifest when any of the jobs fail.
   - *Job Queueing*: Sends jobs to the Job Queue for processing.
   - *Storage Updates*: Collects storage updates suggested by Workers and stores them in object storage for each segment.
   - *Post-processing Cleanup*: Marks requests as processed and removes all the files from the storage.

3. **Job Queue**: The job queue manages job distribution and Worker-Job Builder communication.

   - *Job Distribution*: Sends jobs to available workers via gRPC streaming.
   - *Retry Logic*: Automatically retries failed or timed-out jobs until allowed number of attempts.
   - *Job Response*: Sends the job processing response to Job Builder. Also notifies about failed jobs after running out of retries.

#### Worker mode

Workers connect to the Main Compactor via gRPC to fetch and execute jobs, returning results on the same gRPC stream.
Each worker uses `compactor_grpc_address` under the [common config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#common) to connect to the Main Compactor.

When handling Deletion jobs, a worker downloads the listed chunks, applies the specified filters to rebuild them without the filtered lines, and then provides a comprehensive storage update as job execution response.
The storage update details which chunks to delete from object storage and which newly created chunks to index.

{{< admonition type="note" >}}
Handling of missing chunks differs between the singleton Compactor and horizontally scalable mode. In singleton mode, a missing chunk fails the delete request unless `compactor.deletion-ignore-missing-chunks` is enabled. In horizontally scalable mode, Workers always skip missing chunks and log a warning, and `compactor.deletion-ignore-missing-chunks` has no effect.
{{< /admonition >}}

## Configure compactor mode

The `horizontal_scaling_mode` configuration option in the compactor controls the enablement of horizontally scalable compactor deployment.
It supports setting the following modes:

- **`disabled`** (default): Keeps the horizontal scaling mode disabled. Locally runs all the functions of the compactor.
- **`main`**: Runs all functions of the compactor. Distributes work to workers where possible.
- **`worker`**: Runs the compactor in worker mode, only working on jobs built by the main compactor.

### Configure Main mode

To run the Compactor in Main mode, set the horizontal scaling mode to `main`:

```yaml
compactor:
  # CLI flag: -compactor.horizontal-scaling-mode
  horizontal_scaling_mode: "main"
```

The following configuration options are available for the Main compactor to configure job building:

```yaml
compactor:
  jobs_config:
    deletion:
      # Object storage path prefix for storing deletion manifests.
      # CLI flag: -compactor.jobs.deletion.deletion-manifest-store-prefix
      [deletion_manifest_store_prefix: <string> | default = "__deletion_manifest__/"]
      
      # Maximum time to wait for a job before considering it failed and retrying.
      # CLI flag: -compactor.jobs.deletion.timeout
      [timeout: <duration> | default = 15m]
      
      # Maximum number of times to retry a failed or timed out job.
      # CLI flag: -compactor.jobs.deletion.max-retries
      [max_retries: <int> | default = 3]
```

### Configure Worker mode

To run the Compactor in Worker mode, set the horizontal scaling mode to `worker` and configure the Main compactor's gRPC address:

```yaml
common:
  compactor_grpc_address: <HOST>:<GRPC_PORT>
compactor:
  # CLI flag: -compactor.horizontal-scaling-mode
  horizontal_scaling_mode: "worker"
```

To secure the gRPC connection to the Main compactor, for example with TLS, configure the top-level `compactor_grpc_client` block (CLI flag prefix `-compactor.grpc-client`). Refer to the [`grpc_client` block](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#grpc_client) in the configuration reference for the available options.

The following configuration options are available for the Worker to configure job execution:

```yaml
compactor:
  jobs_config:
    deletion:
      # Maximum number of chunks to process concurrently in each worker.
      # CLI flag: -compactor.jobs.deletion.chunk-processing-concurrency
      [chunk_processing_concurrency: <int> | default = 3]

  worker_config:
    # Number of sub-workers to run for concurrent processing of jobs. This
    # value must be greater than 0, or the compactor fails to start.
    # CLI flag: -compactor.worker.num-sub-workers
    [num_sub_workers: <int> | default = 4]
```

## See also

- [Migrate from SSD to HA monolithic](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/ssd-to-ha-monolithic/) for a worked example of configuring Main and Worker instances.
- [Log entry deletion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/logs-deletion/) for background on the deletion workload that horizontal scaling distributes.
