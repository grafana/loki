---
title: Monitor Loki
description: Describes the various options for monitoring your Loki environment, and the metrics available.
aliases: 
 - ../operations/observability
---

# Monitor Loki

As part of your Loki implementation, you will also want to monitor your Loki cluster.

As a best practice, you should collect data about Loki in a separate instance of Loki, for example, send your Loki data to a [Grafana Cloud account](https://grafana.com/products/cloud/). This will let you troubleshoot a broken Loki cluster from a working one.

Loki exposes the following observability data about itself:

- **Metrics**: Loki provides a `/metrics` endpoint that exports information about Loki in Prometheus format. These metrics provide aggregated metrics of the health of your Loki cluster, allowing you to observe query response times, etc etc.
- **Logs**: Loki emits a detailed log line `metrics.go` for every query, which shows query duration, number of lines returned, query throughput, the specific LogQL that was executed, chunks searched, and much more. You can use these log lines to improve and optimize your query performance.

You can also scrape the Loki logs and metrics and push them to separate instances of Loki and Mimir to provide information about the health of your Loki system (a process known as "meta-monitoring").

The Loki [mixin](https://github.com/grafana/loki/blob/main/production/loki-mixin) is an opinionated set of dashboards, alerts and recording rules to monitor your Loki cluster. The mixin provides a comprehensive package for monitoring Loki in production. You can install the mixin into a Grafana instance.

- To install meta-monitoring using the Loki Helm Chart and Grafana Cloud, follow [these directions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/monitor-and-alert/with-grafana-cloud/).

- To install meta-monitoring using the Loki Helm Chart and a local Loki stack, follow [these directions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/monitor-and-alert/with-local-monitoring/).

- To install the Loki mixin, follow [these directions]({{< relref "./mixins" >}}).

You should also plan separately for infrastructure-level monitoring, to monitor the capacity or throughput of your storage provider, for example, or your networking layer.

- [MinIO](https://min.io/docs/minio/linux/operations/monitoring/collect-minio-metrics-using-prometheus.html)
- [Kubernetes](https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/)

## Loki Metrics

As Loki is a [distributed system](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/), each component exports its own metrics. The `/metrics` endpoint exposes hundreds of different metrics. You can find a sampling of the metrics exposed by Loki and their descriptions, in the sections below.

You can find a complete list of the exposed metrics by checking the `/metrics` endpoint.

`http://<host>:<http_listen_port>/metrics`

For example:

[http://localhost:3100/metrics](http://localhost:3100/metrics)

Both Grafana Loki and Promtail expose a `/metrics` endpoint that expose Prometheus metrics (the default port is 3100 for Loki and 80 for Promtail). You will need a local Prometheus and add Loki and Promtail as targets. See [configuring Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/configuration) for more information.

All components of Loki expose the following metrics:

| Metric Name                        | Metric Type | Description                                                                                                                  |
| ---------------------------------- | ----------- | ----------------------------------------------------------------------- |
| `loki_internal_log_messages_total` | Counter     | Total number of log messages created by Loki itself.                    |
| `loki_request_duration_seconds`    | Histogram   | Number of received HTTP requests.                                       |

Note that most of the metrics are counters and should continuously increase during normal operations.

1. Your app emits a log line to a file that is tracked by Promtail.
1. Promtail reads the new line and increases its counters.
1. Promtail forwards the log line to a Loki distributor, where the received
   counters should increase.
1. The Loki distributor forwards the log line to a Loki ingester, where the
   request duration counter should increase.

If Promtail uses any pipelines with metrics stages, those metrics will also be
exposed by Promtail at its `/metrics` endpoint. See Promtail's documentation on
[Pipelines](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/pipelines/) for more information.

### Metrics cardinality

Some of the Loki observability metrics are emitted per tracked file (active), with the file path included in labels. This increases the quantity of label values across the environment, thereby increasing cardinality. Best practices with Prometheus labels discourage increasing cardinality in this way. Review your emitted metrics before scraping with Prometheus, and configure the scraping to avoid this issue.

## Example Loki log line: metrics.go

Loki emits a "metrics.go" log line from the Querier, Query frontend and Ruler components, which lets you inspect query and recording rule performance. This is an example of a detailed log line "metrics.go" for a query.

Example log

`level=info ts=2024-03-11T13:44:10.322919331Z caller=metrics.go:143 component=frontend org_id=mycompany latency=fast query="sum(count_over_time({kind=\"auditing\"} | json | user_userId =`` [1m]))" query_type=metric range_type=range length=10m0s start_delta=10m10.322900424s end_delta=10.322900663s step=1s duration=47.61044ms status=200 limit=100 returned_lines=0 throughput=9.8MB total_bytes=467kB total_entries=1 queue_time=0s subqueries=2 cache_chunk_req=1 cache_chunk_hit=1 cache_chunk_bytes_stored=0 cache_chunk_bytes_fetched=14394 cache_index_req=19 cache_index_hit=19 cache_result_req=1 cache_result_hit=1`

You can use the query-frontend `metrics.go` lines to understand a query’s overall performance. The “metrics.go” line output by the Queriers contains the same information as the Query frontend but is often more helpful in understanding and troubleshooting query performance. This is largely because it can tell you how the querier spent its time executing the subquery. Here are the most useful stats:

- **total_bytes**: how many total bytes the query processed
- **duration**: how long the query took to execute
- **throughput**: total_bytes/duration
- **total_lines**: how many total lines the query processed
- **length**: how much time the query was executed over
- **post_filter_lines**: how many lines matched the filters in the query
- **cache_chunk_req**: total number of chunks fetched for the query (the cache will be asked for every chunk so this is equivalent to the total chunks requested)
- **splits**: how many pieces the query was split into based on time and split_queries_by_interval
- **shards**: how many shards the query was split into

For more information, refer to the blog post [The concise guide to Loki: How to get the most out of your query performance](https://grafana.com/blog/2023/12/28/the-concise-guide-to-loki-how-to-get-the-most-out-of-your-query-performance/).

### Configure Logging Levels

To change the configuration for Loki logging levels, update log_level configuration parameter in your `config.yaml` file.

```yaml
# Only log messages with the given severity or above. Valid levels: [debug,
# info, warn, error]
# CLI flag: -log.level
[log_level: <string> | default = "info"]
```
