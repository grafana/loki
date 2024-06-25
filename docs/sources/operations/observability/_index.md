---
title: Monitoring Loki
menuTitle:  Observability
description: Observing Grafana Loki
weight: 
---

As part of your Loki implementation, you will also want to monitor your Loki cluster.

As a best practice, you should collect data about Loki in a separate instance of Loki, for example, send your Loki data to a [Grafana Cloud account](https://grafana.com/products/cloud/). This will let you troubleshoot a broken Loki cluster from a working one.

Loki exposes the following observability data about itself:

- **Metrics**: Loki provides a `/metrics` endpoint that exports information about Loki in Prometheus format. These metrics provide aggregated metrics of the health of your Loki cluster, allowing you to observe query response times, etc etc.
- **Logs**: Loki emits a detailed log line "metrics.go" for every query, which shows query duration, number of lines returned, query throughput, the specific LogQL that was executed, chunks searched, and much more. You can use these log lines to improve and optimize your query performance.

You can also scrape Loki's logs and metrics and push them to separate instances of Loki and Mimir to provide information about the health of your Loki system (a process known as "meta-monitoring").

The Loki [mixin](https://github.com/grafana/loki/blob/main/production/loki-mixin) is an opinionated set of dashboards, alerts and recording rules to monitor your Loki cluster. The mixin provides a comprehensive package for monitoring Loki in production. You can install the mixin into a Grafana instance.

- To install meta-monitoring using the Loki Helm Chart and Grafana Cloud, follow [these directions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/monitor-and-alert/with-grafana-cloud/).

- To install meta-monitoring using the Loki Helm Chart and a local Loki stack, follow [these directions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/monitor-and-alert/with-local-monitoring/).

- To install the Loki mixin, follow [these directions](./mixins).

You should also plan separately for infrastructure-level monitoring, to monitor the capacity or throughput of your storage provider, for example, or your networking layer.  (LINK? )
MinIO - https://min.io/docs/minio/linux/operations/monitoring/collect-minio-metrics-using-prometheus.html
Kubernetes - https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/

## Loki Metrics

As Loki is a [distributed system](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/), each component exports its own metrics. The `/metrics` endpoint exposes over a thousand different metrics.  You can find a sampling of the metrics exposed by Loki and their descriptions, in the sections below.

You can find a complete list of the exposed metrics by checking the `/metrics` endpoint.

`http://<host>:<http_listen_port>/metrics`

For example:

`http://localhost:3100/metrics`

Both Grafana Loki and Promtail expose a `/metrics` endpoint that expose Prometheus metrics (the default port is 3100 for Loki and 80 for Promtail). You will need
a local Prometheus and add Loki and Promtail as targets.
See [configuring Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/configuration) for more information.

All components of Loki expose the following metrics:

| Metric Name                        | Metric Type | Description                                                                                                                  |
| ---------------------------------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `loki_log_messages_total`          | Counter     | DEPRECATED. Use internal_log_messages_total for the same functionality. Total number of log messages created by Loki itself. |
| `loki_internal_log_messages_total` | Counter     | Total number of log messages created by Loki itself.                                                                         |
| `loki_request_duration_seconds`    | Histogram   | Number of received HTTP requests.                                                                                            |

The Loki Distributors expose the following metrics:

| Metric Name                                       | Metric Type | Description                                                                                                                          |
| ------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `loki_distributor_ingester_appends_total`         | Counter     | The total number of batch appends sent to ingesters.                                                                                 |
| `loki_distributor_ingester_append_failures_total` | Counter     | The total number of failed batch appends sent to ingesters.                                                                          |
| `loki_distributor_bytes_received_total`           | Counter     | The total number of uncompressed bytes received per both tenant and retention hours.                                                                          |
| `loki_distributor_lines_received_total`           | Counter     | The total number of log _entries_ received per tenant (not necessarily of _lines_, as an entry can have more than one line of text). |

The Loki Ingesters expose the following metrics:

| Metric Name                                  | Metric Type | Description                                                                                               |
| -------------------------------------------- | ----------- | --------------------------------------------------------------------------------------------------------- |
| `loki_ingester_flush_queue_length`           | Gauge       | The total number of series pending in the flush queue.                                                    |
| `loki_chunk_store_index_entries_per_chunk`   | Histogram   | Number of index entries written to storage per chunk.                                                     |
| `loki_ingester_memory_chunks`                | Gauge       | The total number of chunks in memory.                                                                     |
| `loki_ingester_memory_streams`               | Gauge       | The total number of streams in memory.                                                                    |
| `loki_ingester_chunk_age_seconds`            | Histogram   | Distribution of chunk ages when flushed.                                                                  |
| `loki_ingester_chunk_encode_time_seconds`    | Histogram   | Distribution of chunk encode times.                                                                       |
| `loki_ingester_chunk_entries`                | Histogram   | Distribution of lines per-chunk when flushed.                                                             |
| `loki_ingester_chunk_size_bytes`             | Histogram   | Distribution of chunk sizes when flushed.                                                                 |
| `loki_ingester_chunk_utilization`            | Histogram   | Distribution of chunk utilization (filled uncompressed bytes vs maximum uncompressed bytes) when flushed. |
| `loki_ingester_chunk_compression_ratio`      | Histogram   | Distribution of chunk compression ratio when flushed.                                                     |
| `loki_ingester_chunk_stored_bytes_total`     | Counter     | Total bytes stored in chunks per tenant.                                                                  |
| `loki_ingester_chunks_created_total`         | Counter     | The total number of chunks created in the ingester.                                                       |
| `loki_ingester_chunks_stored_total`          | Counter     | Total stored chunks per tenant.                                                                           |
| `loki_ingester_received_chunks`              | Counter     | The total number of chunks sent by this ingester whilst joining during the handoff process.               |
| `loki_ingester_samples_per_chunk`            | Histogram   | The number of samples in a chunk.                                                                         |
| `loki_ingester_sent_chunks`                  | Counter     | The total number of chunks sent by this ingester whilst leaving during the handoff process.               |
| `loki_ingester_streams_created_total`        | Counter     | The total number of streams created per tenant.                                                           |
| `loki_ingester_streams_removed_total`        | Counter     | The total number of streams removed per tenant.                                                           |

The Loki compactor exposes the following metrics:

| Metric Name                                                   | Metric Type | Description                                                                                             |
| ------------------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------- |
| `loki_compactor_delete_requests_processed_total`              | Counter     | Number of delete requests processed per user.                                                           |
| `loki_compactor_delete_requests_chunks_selected_total`        | Counter     | Number of chunks selected while building delete plans per user.                                         |
| `loki_compactor_delete_processing_fails_total`                | Counter     | Number of times the delete phase of compaction has failed.                                                 |
| `loki_compactor_load_pending_requests_attempts_total`         | Counter     | Number of attempts that were made to load pending requests with status.                                 |
| `loki_compactor_oldest_pending_delete_request_age_seconds`    | Gauge       | Age of oldest pending delete request in seconds since they are over their cancellation period.         |
| `loki_compactor_pending_delete_requests_count`                | Gauge       | Count of delete requests which are over their cancellation period and have not finished processing yet. |
| `loki_compactor_deleted_lines`                                | Counter     | Number of deleted lines per user.                                                                       |

Promtail exposes these metrics:

| Metric Name                               | Metric Type | Description                                                                                |
| ----------------------------------------- | ----------- | ------------------------------------------------------------------------------------------ |
| `promtail_read_bytes_total`               | Gauge       | Number of bytes read.                                                                      |
| `promtail_read_lines_total`               | Counter     | Number of lines read.                                                                      |
| `promtail_dropped_bytes_total`            | Counter     | Number of bytes dropped because failed to be sent to the ingester after all retries.       |
| `promtail_dropped_entries_total`          | Counter     | Number of log entries dropped because failed to be sent to the ingester after all retries. |
| `promtail_encoded_bytes_total`            | Counter     | Number of bytes encoded and ready to send.                                                 |
| `promtail_file_bytes_total`               | Gauge       | Number of bytes read from files.                                                           |
| `promtail_files_active_total`             | Gauge       | Number of active files.                                                                    |
| `promtail_request_duration_seconds` | Histogram   | Number of send requests.                                                                   |
| `promtail_sent_bytes_total`               | Counter     | Number of bytes sent.                                                                      |
| `promtail_sent_entries_total`             | Counter     | Number of log entries sent to the ingester.                                                |
| `promtail_targets_active_total`           | Gauge       | Number of total active targets.                                                            |
| `promtail_targets_failed_total`           | Counter     | Number of total failed targets.                                                            |

Most of these metrics are counters and should continuously increase during normal operations:

1. Your app emits a log line to a file that is tracked by Promtail.
2. Promtail reads the new line and increases its counters.
3. Promtail forwards the log line to a Loki distributor, where the received
   counters should increase.
4. The Loki distributor forwards the log line to a Loki ingester, where the
   request duration counter should increase.

If Promtail uses any pipelines with metrics stages, those metrics will also be
exposed by Promtail at its `/metrics` endpoint. See Promtail's documentation on
[Pipelines]({{< relref "../send-data/promtail/pipelines" >}}) for more information.

An example Grafana dashboard was built by the community and is available as
dashboard [10004](/dashboards/10004).

## Metrics cardinality

Some of the Loki observability metrics are emitted per tracked file (active), with the file path included in labels.
This increases the quantity of label values across the environment, thereby increasing cardinality. Best practices with Prometheus [labels](https://prometheus.io/docs/practices/naming/#labels) discourage increasing cardinality in this way.
Review your emitted metrics before scraping with Prometheus, and configure the scraping to avoid this issue.
