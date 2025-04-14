---
title: Loki meta-monitoring
menuTitle: Monitor Loki
description: Describes the various options for monitoring your Loki environment, and the metrics available.
aliases: 
 - ../operations/observability
---
# Loki meta-monitoring

As part of your Loki implementation, you will also want to monitor your Loki cluster.

As a best practice, you should collect data about Loki in a separate instance of Loki, Prometheus, and Grafana. For example, send your Loki cluster data to a [Grafana Cloud account](https://grafana.com/products/cloud/). This will let you troubleshoot a broken Loki cluster from a working one.

Loki exposes the following observability data about itself:

- **Metrics**: Loki provides a `/metrics` endpoint that sends information about Loki in Prometheus format. These metrics provide aggregated metrics of the health of your Loki cluster, allowing you to observe query response times, etc. Each Loki component sends its own metrics, allowing for fine-grained monitoring of the health of your Loki cluster. For more information about the metrics Loki exposes, refer to [metrics](#loki-metrics). It is important to keep [metrics cardinality](#metrics-cardinality) in mind when running a large distributed Loki cluster.
  
- **Logs**: Loki emits a detailed log line `metrics.go` for every query, which shows query duration, number of lines returned, query throughput, the specific LogQL that was executed, chunks searched, and much more. You can use these log lines to improve and optimize your query performance. You can also collect pod logs from your Loki components to monitor and drill down into specific issues.

## Monitoring Loki

There are three primary components to monitoring Loki:

1. [Kubernetes Monitoring Helm](https://github.com/grafana/k8s-monitoring-helm/): The Kubernetes Monitoring Helm chart provides a comprehensive monitoring solution for Kubernetes clusters. It also provides direct integrations for monitoring the full LGTM (Loki, Grafana, Tempo and Mimir) stack. To learn how to deploy the Kubernetes Monitoring Helm chart, refer to [deploy meta-monitoring](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/deploy).

1. [Grafana Cloud account](https://grafana.com/products/cloud/) or a seperate LGTM stack: The data collected from the Loki cluster can be sent to a Grafana Cloud account or a separate LGTM stack. We recommend using Grafana Cloud since it is Grafana Lab's responsiblity to maintain the availability and performance of the Grafana Cloud services. 

1. [The Loki mixin](https://github.com/grafana/loki/tree/main/production/loki-mixin-compiled): is an opinionated set of dashboards, alerts, and recording rules to monitor your Loki cluster. The mixin provides a comprehensive package for monitoring Loki in production. You can install the mixin into a Grafana instance. To install the Loki mixin, follow [these directions](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/mixins).

You should also plan separately for infrastructure-level monitoring, to monitor the capacity or throughput of your storage provider, for example, or your networking layer.

- [MinIO](https://min.io/docs/minio/linux/operations/monitoring/collect-minio-metrics-using-prometheus.html)
- [Kubernetes](https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/)

The Kubernetes Monitoring Helm chart Grafana Labs uses to monitor Loki also provides these features out of the box with Kubernetes monitoring enabled by default. You can choose which of these features to enable or disable based on how much data you want to collect and your meta-monitoring budget.

## Loki Metrics

As Loki is a [distributed system](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/), each component exports its own metrics. The `/metrics` endpoint exposes hundreds of different metrics. You can find a sampling of the metrics exposed by Loki and their descriptions, in the sections below.

You can find a complete list of the exposed metrics by checking the `/metrics` endpoint.

`http://<host>:<http_listen_port>/metrics`

For example:

[http://localhost:3100/metrics](http://localhost:3100/metrics)

Both Grafana Loki and Alloy expose a `/metrics` endpoint that expose Prometheus metrics (the default port is `3100` for Loki and `12345` for Alloy). To store these metrics, you can use Prometheus or Mimir.

All components of Loki expose the following metrics:

| Metric Name                        | Metric Type | Description                                                                                                                  |
| ---------------------------------- | ----------- | ----------------------------------------------------------------------- |
| `loki_internal_log_messages_total` | Counter     | Total number of log messages created by Loki itself.                    |
| `loki_request_duration_seconds`    | Histogram   | Number of received HTTP requests.                                       |

Note that most of the metrics are counters and should continuously increase during normal operations.

1. Your app emits a log line to a file that is tracked by Alloy.
1. Alloy reads the new line and increases its counters.
1. Alloy forwards the log line to a Loki distributor, where the received
   counters should increase.
1. The Loki distributor forwards the log line to a Loki ingester, where the
   request duration counter should increase.

If Alloy uses any pipelines with metrics stages, those metrics will also be
exposed by Alloy at its `/metrics` endpoint.

### Metrics cardinality

Some of the Loki observability metrics are emitted per tracked file (active), with the file path included in labels. This increases the quantity of label values across the environment, thereby increasing cardinality. Best practices with Prometheus labels discourage increasing cardinality in this way. The Kubernetes Monitoring Helm chart provides best practices for monitoring Loki, including how to manage cardinality, however it is important to be aware of cardinality when implementing auto scaling of components in your Loki cluster.

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
