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
[Grafana Mimir query-scheduler](https://grafana.com/docs/mimir/latest/operators-guide/architecture/components/query-scheduler/). This allows running multiple query frontends.

To run with the Query Scheduler, configure both the frontend and the querier worker with the scheduler's address.

Using CLI flags:

- Frontend: `-frontend.scheduler-address=<scheduler-host:port>`
- Querier: `-querier.scheduler-address=<scheduler-host:port>`

Using the configuration file:

```yaml
frontend:
  scheduler_address: <scheduler-host:port>

frontend_worker:
  scheduler_address: <scheduler-host:port>
```

{{< admonition type="note" >}}
The querier's `scheduler_address` is configured under `frontend_worker`, not under `querier`. The [`frontend_worker`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#frontend_worker) block configures the worker running within the querier that pulls and executes queries.
{{< /admonition >}}

It is not valid to start the querier with both a configured frontend address and a scheduler address.

The query scheduler process itself can be started via the `-target=query-scheduler` option of the Loki Docker image. For instance, `docker run grafana/loki:latest -config.file=/etc/loki/config.yaml -target=query-scheduler -server.http-listen-port=8009 -server.grpc-listen-port=9009` starts the query scheduler listening on ports `8009` and `9009`.

### Scheduler discovery using a ring

Instead of configuring a static `scheduler_address` on the frontend and the querier worker, you can have query schedulers register themselves in a [hash ring](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/hash-rings/#about-the-query-scheduler-ring). Queriers and query frontends then discover query schedulers through the ring instead of using a fixed address.

To enable this, set `use_scheduler_ring: true`, or use the `-query-scheduler.use-scheduler-ring` CLI flag:

```yaml
query_scheduler:
  use_scheduler_ring: true
```

{{< admonition type="note" >}}
Set this option in the configuration used by your query schedulers, your queriers, and your query frontends. Queriers and query frontends only read the ring if `use_scheduler_ring` is true in their own configuration. If all of your components share one configuration file, you only need to set it once.
{{< /admonition >}}

The ring needs a key-value store. In most deployments you do not need to configure one for the query scheduler specifically, because the scheduler ring inherits the store from the [`common.ring`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#common) block, and Loki uses `memberlist` for all rings when you configure a `memberlist` section. To set the store for the scheduler ring alone, use the `scheduler_ring` block:

```yaml
query_scheduler:
  use_scheduler_ring: true
  scheduler_ring:
    kvstore:
      store: memberlist
```

If you do not configure a frontend address, a scheduler address, or a downstream URL anywhere in your configuration, Loki automatically enables the scheduler ring for you.

Each component that takes part in the ring exposes the ring state at the `/scheduler/ring` endpoint, which you can use to check that all query schedulers registered as expected.

## Memory ballast

In compute-constrained environments, garbage collection can become a significant performance factor. Frequently-run garbage collection interferes with running the application by using CPU resources. The use of memory ballast can mitigate the issue. Memory ballast allocates extra, but unused virtual memory in order to inflate the quantity of live heap space. Garbage collection is triggered by the growth of heap space usage. The inflated quantity of heap space reduces the perceived growth, so garbage collection occurs less frequently.

Configure memory ballast using the ballast_bytes configuration option.

## Go memory limit

On startup, Loki reads the memory limit of the cgroup it runs in and sets the Go runtime soft memory limit, `GOMEMLIMIT`, to 90% of that limit. The garbage collector then works to keep the heap below this value, which lowers the risk that the container is terminated for excessive memory use. Loki logs the value it sets.

The limit follows the memory limit of the container, so you do not have to update a configuration value when you change that limit, for example with a Kustomize overlay or a vertical pod autoscaler.

Two environment variables control this behavior. Loki reads them from the process environment at startup. They are not configuration file options or CLI flags, so they do not appear in the [Configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/).

| Variable | Default | Description |
| --- | --- | --- |
| `GOMEMLIMIT` | Unset | Sets the limit directly, for example `4GiB`. When set, Loki keeps this value and does not read the cgroup limit. For the accepted format, refer to the [Go runtime documentation](https://pkg.go.dev/runtime#hdr-Environment_Variables). |
| `AUTOMEMLIMIT` | Unset, which uses `0.9` | Sets the fraction of the cgroup limit to use, in the range `(0.0,1.0]`, for example `0.85`. Set it to `off` to leave `GOMEMLIMIT` unset. |

If Loki does not run in a cgroup with a memory limit, the limit stays unset and the heap can grow without a target.

If a component has a memory limit in its Helm chart `resources` value, the Loki Helm chart sets `GOMEMLIMIT` for it to 85% of that limit, and Loki keeps the value from the chart. The `defaults.goSettings.goMemLimitFactor` value sets this fraction. For details, refer to the [Helm Chart Reference](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/reference/).

{{< admonition type="note" >}}
A soft memory limit makes garbage collection more frequent as the heap approaches the limit. It does not prevent termination if live heap memory exceeds the limit.
{{< /admonition >}}

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

If the `query-frontend` connection requires TLS, set `tls_enabled: true` under `query_frontend` and configure the accompanying TLS options.

To reduce contention when many rules evaluate at the same time, set `max_jitter` to add a bounded, random delay before each rule evaluation. This option applies to both local and remote evaluation modes:

```yaml
ruler:
  evaluation:
    max_jitter: 5s
```

Refer to the [`ruler` configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#ruler) for further configuration options.

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

Both of these can be specified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) section
or on a [per-tenant basis](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime-configuration-file).

`max_jitter`, described earlier in this topic, is a global-only setting under `ruler.evaluation` rather than a per-tenant limit.

Remote rule evaluation exposes a number of metrics:

- `loki_ruler_remote_eval_request_duration_seconds`: time taken for rule evaluation (histogram)
- `loki_ruler_remote_eval_response_bytes`: number of bytes in rule evaluation response (histogram)
- `loki_ruler_remote_eval_response_samples`: number of samples in rule evaluation response (histogram)
- `loki_ruler_remote_eval_success_total`: successful rule evaluations (counter)
- `loki_ruler_remote_eval_failure_total`: unsuccessful rule evaluations with reasons (counter)

Each of these metrics are per-tenant, so cardinality must be taken into consideration.
