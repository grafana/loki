---
title: Manage larger production deployments
menuTitle: Scale Loki
description: Describes strategies how to scale a Loki deployment when log volume increases.
weight: 
---
# Manage larger production deployments

When needing to scale Loki due to increased log volume, operators should consider running several Loki processes
partitioned by role (ingester, distributor, querier, and so on) rather than a single Loki
process. Grafana Labs' [production setup](https://github.com/grafana/loki/blob/main/production/ksonnet/loki)
contains `.libsonnet` files that demonstrates configuring separate components
and scaling for resource usage.

## Separate Query Scheduler

The Query frontend has an in-memory queue that can be moved out into a separate process similar to the
[Grafana Mimir query-scheduler](/docs/mimir/latest/operators-guide/architecture/components/query-scheduler/). This allows running multiple query frontends.

To run with the Query Scheduler, the frontend needs to be passed the scheduler's address via `-frontend.scheduler-address` and the querier processes needs to be started with `-querier.scheduler-address` set to the same address. Both options can also be defined via the [configuration file](https://grafana.com/docs/loki/<LOKI_VERSION>/configure).

It is not valid to start the querier with both a configured frontend and a scheduler address.

The query scheduler process itself can be started via the `-target=query-scheduler` option of the Loki Docker image. For instance, `docker run grafana/loki:latest -config.file=/etc/loki/config.yaml -target=query-scheduler -server.http-listen-port=8009 -server.grpc-listen-port=9009` starts the query scheduler listening on ports `8009` and `9009`.

## Memory ballast

In compute-constrained environments, garbage collection can become a significant performance factor. Frequently-run garbage collection interferes with running the application by using CPU resources. The use of memory ballast can mitigate the issue. Memory ballast allocates extra, but unused virtual memory in order to inflate the quantity of live heap space. Garbage collection is triggered by the growth of heap space usage. The inflated quantity of heap space reduces the perceived growth, so garbage collection occurs less frequently.

Configure memory ballast using the ballast_bytes configuration option.

## Remote rule evaluation

_This feature was first proposed in [`LID-0002`](https://github.com/grafana/loki/pull/8129); it contains the design decisions
which informed the implementation._

By default, the `ruler` component embeds a query engine to evaluate rules. This generally works fine, except when rules
are complex or have to process a large amount of data regularly. Poor performance of the `ruler` manifests as recording rules metrics
with gaps or missed alerts. This situation can be detected by alerting on the `loki_prometheus_rule_group_iterations_missed_total` metric
when it has a non-zero value.

A solution to this problem is to externalize rule evaluation from the `ruler` process. The `ruler` embedded query engine
is single-threaded, meaning that rules are not split, sharded, or otherwise accelerated like regular Loki queries. The `query-frontend`
component exists explicitly for this purpose and, when combined with a number of `querier` instances, can massively
improve rule evaluation performance and lead to fewer missed iterations.

It is generally recommended to create a separate `query-frontend` deployment and `querier` pool from your existing one - which handles adhoc
queries via Grafana, `logcli`, or the API. Rules should be given priority over adhoc queries because they are used to produce
metrics or alerts which may be crucial to the reliable operation of your service; if you use the same `query-frontend` and `querier` pool
for both, your rules will be executed with the same priority as adhoc queries which could lead to unpredictable performance.

To enable remote rule evaluation, set the following configuration options:

```yaml
ruler:
  evaluation:
    mode: remote
    query_frontend:
      address: dns:///<query-frontend-service>:<grpc-port>
```

See [`here`](/configuration/#ruler) for further configuration options.

When you enable remote rule evaluation, the `ruler` component becomes a gRPC client to the `query-frontend` service; 
this will result in far lower `ruler` resource usage because the majority of the work has been externalized.
The LogQL queries coming from the `ruler` will be executed against the given `query-frontend` service.
Requests will be load-balanced across all `query-frontend` IPs if the `dns:///` prefix is used.

{{< admonition type="note" >}}
Queries that fail to execute are _not_ retried.
{{< /admonition >}}

### Limits and Observability

Remote rule evaluation can be tuned with the following options:

- `ruler_remote_evaluation_timeout`: maximum allowable execution time for rule evaluations
- `ruler_remote_evaluation_max_response_size`: maximum allowable response size over gRPC connection from `query-frontend` to `ruler`

Both of these can be specified globally in the [`limits_config`](/configuration/#limits_config) section
or on a [per-tenant basis](/configuration/#runtime-configuration-file). 

Remote rule evaluation exposes a number of metrics:

- `loki_ruler_remote_eval_request_duration_seconds`: time taken for rule evaluation (histogram)
- `loki_ruler_remote_eval_response_bytes`: number of bytes in rule evaluation response (histogram)
- `loki_ruler_remote_eval_response_samples`: number of samples in rule evaluation response (histogram)
- `loki_ruler_remote_eval_success_total`: successful rule evaluations (counter)
- `loki_ruler_remote_eval_failure_total`: unsuccessful rule evaluations with reasons (counter)

Each of these metrics are per-tenant, so cardinality must be taken into consideration.
