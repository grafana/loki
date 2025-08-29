---
title: Horizontal scaling of Compactor
menuTitle: Horizontal scaling of Compactor
description: Describes working of horizontally scalable compactor and its configurations.
weight: 800
---
# Introduction

{{< admonition type="caution" >}}
Compactor horizontal scaling is an experimental feature. Use it with caution in your production environments.
{{< /admonition >}}

Grafana Loki saw a major improvement in its operational complexity and cost-effectiveness with the introduction of object-storage-based indexes.
This change also led to the addition of a singleton Compactor service, initially responsible only for index compaction.
However, as new features like [Custom Retention](../retention) and [Deletion of Logs with line filters](../logs-deletion) were introduced, the Compactor's responsibilities grew.
With increasing scale and more demanding features, especially log deletion with line filters, the singleton Compactor began to show its scaling limits.

Now, you can run the Loki Compactor in a horizontally scalable mode.
Since log deletion with line filters is the compactor's most operationally intensive work, initially,
this horizontally scalable architecture will be leveraged to speed up and distribute the workload specifically for its log deletion with line filters functionality.

# How it works

We have introduced two new modes to the Compactor for operating in horizontally scalable mode:
1. Main: 
   * Runs all Compactor functions and distributes chunk processing for log line deletion with filters to the workers.
   * Should be deployed as a singleton with access to a disk, like the current singleton deployment.
2. Worker:
   * Connects to the Main Compactor over gRPC to get and execute the jobs.
   * Multiple replicas can be deployed to achieve higher job processing throughput.
   * Only needs access to Object Storage for reading/writing chunks.

## Implementation details

Although, Horizontally Scalable Compactor currently only supports distributing the work of log line deletion with filters,
in the future, we might add support for distributing more kind of work to the Workers.
We are going to use the current functionality to see in detail some of the implementation details.

### Working of Main mode
For Distribution of Chunk processing work from Main compactor, there are following core components involved:

1. **Deletion Manifest Builder**: The manifest builder works on a batch of delete request to discover all the chunks covered by them based on their labels filters and time range.
Using the discovered chunks, it creates structured manifests and stores it to the Object Storage. Here is what comprises Deletion Manifest:

   - *Segments*: Groups of up to 100K chunks per segment. Also acts as partition for chunks by Loki Tenant/Table.
   - *Manifest*: Complete metadata about all segments and requests to process.

2. **Job Builder**: The job builder converts manifests into discrete jobs and manages their lifecycle:

   - *Job Creation*: Breaks segments into jobs of up to 1K chunks each. Also includes line filters to apply on the chunks to remove the log lines requested for deletion.
   - *Progress Tracking*: Monitors job completion and stops processing of manifest when any of the jobs fail.
   - *Job Queueing*: Sends jobs to the Job Queue for processing.
   - *Storage Updates*: Collects storage updates suggested by Workers and stores them to Object Storage for each segment.
   - *Post-processing Cleanup*: Marks requests as processed and removes all the files from the storage.

3. **Job Queue**: The job queue manages job distribution and Worker-Job Builder communication.

   - *Job Distribution*: Sends jobs to available workers via gRPC streaming.
   - *Retry Logic*: Automatically retries failed or timed-out jobs until allowed number of attempts.
   - *Job Response*: Sends the job processing response to Job Builder. Also notifies about failed jobs after running out of retries.

### Working of Worker mode

Workers connect to the Main Compactor via gRPC to fetch and execute jobs, returning results on the same gRPC stream.
It uses `compactor_grpc_address` under the [common config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#common) to connect to the Main Compactor.

When handling Deletion jobs, a worker downloads the listed chunks, applies the specified filters to rebuild them without the filtered lines, and then provides a comprehensive storage update as job execution response.
The storage update details which chunks to delete from Object Storage and which newly created chunks to index.

### Sequence diagram

The sequence diagram depicts Deletion Manifest Builder, Job Builder and Job Queue as separate entities than the Main Compactor to show how the components are interlinked.
In reality, all three components run within the Main Compactor.

{{< figure src="../compactor-HS-seq-diagram.png" alt="Compactor Horizontal Scaling Sequence Diagram">}}

## Configuration

The `horizontal_scaling_mode` configuration option in the compactor controls the enablement of horizontally scalable compactor deployment.
It supports setting the following modes:

- **`disabled`** (default): Keeps the horizontal scaling mode disabled. Locally runs all the functions of the compactor.
- **`main`**: Runs all functions of the compactor. Distributes work to workers where possible.
- **`worker`**: Runs the compactor in worker mode, only working on jobs built by the main compactor.

### Config for Main mode

To run Compactor in Main mode, the Horizontal Scaling Mode needs to be set to "main":
```yaml
compactor:
  # CLI flag: -compactor.horizontal-scaling-mode="main"
  horizontal_scaling_mode: "main"
```

Additionally, there are the below config options available for the Main compactor to configure some aspects of Job Building:

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

### Config for Main mode

To run Compactor in Worker mode, the Horizontal Scaling Mode needs to be set to "worker" and Main compactor's GRPC address needs to be set:
```yaml
common:
   compactor_grpc_address: <HOST>:<GRPC PORT>
compactor:
  # CLI flag: -compactor.horizontal-scaling-mode="worker"
  horizontal_scaling_mode: "worker"
```

Additionally, there are the below config options available for the Worker to configure some aspects of Job execution:

```yaml
compactor:
   jobs_config:
      deletion:
         # Maximum number of chunks to process concurrently in each worker.
         # CLI flag: -compactor.jobs.deletion.chunk-processing-concurrency
         [chunk_processing_concurrency: <int> | default = 3]

worker_config:
   # Number of sub-workers to run for concurrent processing of jobs. Setting it to 0
   # will run a subworker per available CPU core.
   # CLI flag: -compactor.worker.num-sub-workers
   [num_sub_workers: <int> | default = 0]
```