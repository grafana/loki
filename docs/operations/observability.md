# Observing Loki

Both Loki and Promtail expose a `/metrics` endpoint that expose Prometheus
metrics. You will need a local Prometheus and add Loki and Promtail as targets.
See [configuring
Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/configuration)
for more information.

All components of Loki expose the following metrics:

| Metric Name                     | Metric Type | Description                              |
| ------------------------------- | ----------- | ---------------------------------------- |
| `log_messages_total`            | Counter     | Total number of messages logged by Loki. |
| `loki_request_duration_seconds` | Histogram   | Number of received HTTP requests.        |

The Loki Distributors expose the following metrics:

| Metric Name                                       | Metric Type | Description                                                 |
| ------------------------------------------------- | ----------- | ----------------------------------------------------------- |
| `loki_distributor_ingester_appends_total`         | Counter     | The total number of batch appends sent to ingesters.        |
| `loki_distributor_ingester_append_failures_total` | Counter     | The total number of failed batch appends sent to ingesters. |
| `loki_distributor_bytes_received_total`           | Counter     | The total number of uncompressed bytes received per tenant. |
| `loki_distributor_lines_received_total`           | Counter     | The total number of lines received per tenant.              |

The Loki Ingesters expose the following metrics:

| Metric Name                                  | Metric Type | Description                                                                                 |
| -------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------- |
| `cortex_ingester_flush_queue_length`         | Gauge       | The total number of series pending in the flush queue.                                      |
| `cortex_chunk_store_index_entries_per_chunk` | Histogram   | Number of index entries written to storage per chunk.                                       |
| `loki_ingester_memory_chunks`                | Gauge       | The total number of chunks in memory.                                                       |
| `loki_ingester_memory_streams`               | Gauge       | The total number of streams in memory.                                                      |
| `loki_ingester_chunk_age_seconds`            | Histogram   | Distribution of chunk ages when flushed.                                                    |
| `loki_ingester_chunk_encode_time_seconds`    | Histogram   | Distribution of chunk encode times.                                                         |
| `loki_ingester_chunk_entries`                | Histogram   | Distribution of lines per-chunk when flushed.                                               |
| `loki_ingester_chunk_size_bytes`             | Histogram   | Distribution of chunk sizes when flushed.                                                   |
| `loki_ingester_chunk_utilization`            | Histogram   | Distribution of chunk utilization (filled uncompressed bytes vs maximum uncompressed bytes) when flushed.                                             |
| `loki_ingester_chunk_compression_ratio`      | Histogram   | Distribution of chunk compression ratio when flushed.                                       |
| `loki_ingester_chunk_stored_bytes_total`     | Counter     | Total bytes stored in chunks per tenant.                                                    |
| `loki_ingester_chunks_created_total`         | Counter     | The total number of chunks created in the ingester.                                         |
| `loki_ingester_chunks_stored_total`          | Counter     | Total stored chunks per tenant.                                                             |
| `loki_ingester_received_chunks`              | Counter     | The total number of chunks sent by this ingester whilst joining during the handoff process. |
| `loki_ingester_samples_per_chunk`            | Histogram   | The number of samples in a chunk.                                                           |
| `loki_ingester_sent_chunks`                  | Counter     | The total number of chunks sent by this ingester whilst leaving during the handoff process. |
| `loki_ingester_streams_created_total`        | Counter     | The total number of streams created per tenant.                                             |
| `loki_ingester_streams_removed_total`        | Counter     | The total number of streams removed per tenant.                                             |

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
| `promtail_log_entries_bytes`              | Histogram   | The total count of bytes read.                                                             |
| `promtail_request_duration_seconds_count` | Histogram   | Number of send requests.                                                                   |
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
[Pipelines](../clients/promtail/pipelines.md) for more information.

An example Grafana dashboard was built by the community and is available as
dashboard [10004](https://grafana.com/dashboards/10004).

## Mixins

The Loki repository has a [mixin](../../production/loki-mixin) that includes a
set of dashboards, recording rules, and alerts. Together, the mixin gives you a
comprehensive package for monitoring Loki in production.

For more information about mixins, take a look at the docs for the
[monitoring-mixins project](https://github.com/monitoring-mixins/docs).


