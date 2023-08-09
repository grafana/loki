---
title: Alerting and recording rules
menuTitle: Alert
description: Learn how the rule evaluates queries for alerting.
aliases:
  - ./rules/
  - ./alerting/
weight: 850
keywords:
  - loki
  - alert
  - alerting
  - ruler
---

# Alerting and recording rules

Grafana Loki includes a component called the ruler. The ruler is responsible for continually evaluating a set of configurable queries and performing an action based on the result.

This example configuration sources rules from a local disk.

[Ruler storage](#ruler-storage) provides further details.

```yaml
ruler:
  storage:
    type: local
    local:
      directory: /tmp/rules
  rule_path: /tmp/scratch
  alertmanager_url: http://localhost
  ring:
    kvstore:
      store: inmemory
  enable_api: true

```

We support two kinds of rules: [alerting](#alerting-rules) rules and [recording](#recording-rules) rules.

## Alerting Rules

We support [Prometheus-compatible](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/) alerting rules. From Prometheus' documentation:

> Alerting rules allow you to define alert conditions based on Prometheus expression language expressions and to send notifications about firing alerts to an external service.

Loki alerting rules are exactly the same, except they use LogQL for their expressions.

### Example

A complete example of a rules file:

```yaml
groups:
  - name: should_fire
    rules:
      - alert: HighPercentageError
        expr: |
          sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)
            /
          sum(rate({app="foo", env="production"}[5m])) by (job)
            > 0.05
        for: 10m
        labels:
            severity: page
        annotations:
            summary: High request latency
  - name: credentials_leak
    rules: 
      - alert: http-credentials-leaked
        annotations: 
          message: "{{ $labels.job }} is leaking http basic auth credentials."
        expr: 'sum by (cluster, job, pod) (count_over_time({namespace="prod"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)'
        for: 10m
        labels: 
          severity: critical
```

## Recording Rules

We support [Prometheus-compatible](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/#recording-rules) recording rules. From Prometheus' documentation:

> Recording rules allow you to precompute frequently needed or computationally expensive expressions and save their result as a new set of time series.

> Querying the precomputed result will then often be much faster than executing the original expression every time it is needed. This is especially useful for dashboards, which need to query the same expression repeatedly every time they refresh.

Loki allows you to run [metric queries]({{< relref "../query/metric_queries" >}}) over your logs, which means
that you can derive a numeric aggregation from your logs, like calculating the number of requests over time from your NGINX access log.

### Example

```yaml
name: NginxRules
interval: 1m
rules:
  - record: nginx:requests:rate1m
    expr: |
      sum(
        rate({container="nginx"}[1m])
      )
    labels:
      cluster: "us-central1"
```

This query (`expr`) will be executed every 1 minute (`interval`), the result of which will be stored in the metric
name we have defined (`record`). This metric named `nginx:requests:rate1m` can now be sent to Prometheus, where it will be stored
just like any other metric.


### Limiting Alerts and Recording Rule Samples

Like [Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/#limiting-alerts-and-series), you can configure a limit for alerts produced by alerting rules and samples produced by recording rules. This limit can be configured per-group. Using limits can prevent a faulty rule from generating a large number of alerts or recording samples. When the limit is exceeded, all recording samples produced by the rule are discarded, and if it is an alerting rule, all alerts for the rule, active, pending, or inactive, are cleared. The event will be recorded as an error in the evaluation, and the rule health will be set to `err`. The default value for limit is `0` meaning no limit.

Here is an example of a rule group along with its limit configured.
#### Example


```yaml
groups:
  - name: production_rules
    limit: 10
    interval: 1m
    rules:
      - alert: HighPercentageError
        expr: |
          sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)
            /
          sum(rate({app="foo", env="production"}[5m])) by (job)
            > 0.05
        for: 10m
        labels:
            severity: page
        annotations:
            summary: High request latency
      - record: nginx:requests:rate1m
        expr: |
          sum(
            rate({container="nginx"}[1m])
          )
        labels:
          cluster: "us-central1"
```

### Remote-Write

With recording rules, you can run these metric queries continually on an interval, and have the resulting metrics written
to a Prometheus-compatible remote-write endpoint. They produce Prometheus metrics from log entries.

At the time of writing, these are the compatible backends that support this:

- [Prometheus](https://prometheus.io/docs/prometheus/latest/disabled_features/#remote-write-receiver) (`>=v2.25.0`):
  Prometheus is generally a pull-based system, but since `v2.25.0` has allowed for metrics to be written directly to it as well.
- [Grafana Mimir](/docs/mimir/latest/operators-guide/reference-http-api/#remote-write)
- [Thanos (`Receiver`)](https://thanos.io/tip/components/receive.md/)

Here is an example remote-write configuration for sending to a local Prometheus instance:

```yaml
ruler:
  ... other settings ...
  
  remote_write:
    enabled: true
    client:
      url: http://localhost:9090/api/v1/write
```

Further configuration options can be found under [ruler]({{< relref "../configure#ruler" >}}).

### Operations

Please refer to the [Recording Rules]({{< relref "../operations/recording-rules" >}}) page.

## Use cases

The Ruler's Prometheus compatibility further accentuates the marriage between metrics and logs. For those looking to get started with metrics and alerts based on logs, or wondering why this might be useful, here are a few use cases we think fit very well.

### Black box monitoring

We don't always control the source code of applications we run. Load balancers and a myriad of other components, both open source and closed third-party, support our applications while they don't expose the metrics we want. Some don't expose any metrics at all. Loki's alerting and recording rules can produce metrics and alert on the state of the system, bringing the components into our observability stack by using the logs. This is an incredibly powerful way to introduce advanced observability into legacy architectures.

### Event alerting

Sometimes you want to know whether _any_ instance of something has occurred. Alerting based on logs can be a great way to handle this, such as finding examples of leaked authentication credentials:
```yaml
- name: credentials_leak
  rules: 
    - alert: http-credentials-leaked
      annotations: 
        message: "{{ $labels.job }} is leaking http basic auth credentials."
      expr: 'sum by (cluster, job, pod) (count_over_time({namespace="prod"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)'
      for: 10m
      labels: 
        severity: critical
```

### Alerting on high-cardinality sources

Another great use case is alerting on high cardinality sources. These are things which are difficult/expensive to record as metrics because the potential label set is huge. A great example of this is per-tenant alerting in multi-tenanted systems like Loki. It's a common balancing act between the desire to have per-tenant metrics and the cardinality explosion that ensues (adding a single _tenant_ label to an existing Prometheus metric would increase its cardinality by the number of tenants).

Creating these alerts in LogQL is attractive because these metrics can be extracted at _query time_, meaning we don't suffer the cardinality explosion in our metrics store.

> **Note** As an example, we can use LogQL v2 to help Loki to monitor _itself_, alerting us when specific tenants have queries that take longer than 10s to complete! To do so, we'd use the following query: `sum by (org_id) (rate({job="loki-prod/query-frontend"} |= "metrics.go" | logfmt | duration > 10s [1m]))`

## Interacting with the Ruler

Because the rule files are identical to Prometheus rule files, we can interact with the Loki Ruler via [`cortextool`](https://github.com/grafana/cortex-tools#rules). The CLI is in early development, but it works with both Loki and Cortex. Pass the `--backend=loki` option when using it with Loki.

> **Note:** Not all commands in cortextool currently support Loki.

> **Note:** cortextool was intended to run against multi-tenant Loki, commands need an `--id=` flag set to the Loki instance ID or set the environment variable `CORTEX_TENANT_ID`.  If Loki is running in single tenant mode, the required ID is `fake` (yes we know this might seem alarming but it's totally fine, no it can't be changed) 

An example workflow is included below:

```sh
# lint the rules.yaml file ensuring it's valid and reformatting it if necessary
cortextool rules lint --backend=loki ./output/rules.yaml

# diff rules against the currently managed ruleset in Loki
cortextool rules diff --rule-dirs=./output --backend=loki

# ensure the remote ruleset matches your local ruleset, creating/updating/deleting remote rules which differ from your local specification.
cortextool rules sync --rule-dirs=./output --backend=loki

# print the remote ruleset
cortextool rules print --backend=loki
```

There is also a [github action](https://github.com/grafana/cortex-rules-action) available for `cortex-tool`, so you can add it into your CI/CD pipelines!

For instance, you can sync rules on master builds via
```yaml
name: sync-cortex-rules-and-alerts
on:
  push:
    branches:
      - master
env:
  CORTEX_ADDRESS: '<fill me in>'
  CORTEX_TENANT_ID: '<fill me in>'
  CORTEX_API_KEY: ${{ secrets.API_KEY }}
  RULES_DIR: 'output/'
jobs:
  sync-loki-alerts:
    runs-on: ubuntu-18.04
    steps:
      - name: Lint Rules
        uses: grafana/cortex-rules-action@v0.4.0
        env:
          ACTION: 'lint'
        with:
          args: --backend=loki
      - name: Diff rules
        uses: grafana/cortex-rules-action@v0.4.0
        env:
          ACTION: 'diff'
        with:
          args: --backend=loki
      - name: Sync rules
        if: ${{ !contains(steps.diff-rules.outputs.detailed, 'no changes detected') }}
        uses: grafana/cortex-rules-action@v0.4.0
        env:
          ACTION: 'sync'
        with:
          args: --backend=loki
      - name: Print rules
        uses: grafana/cortex-rules-action@v0.4.0
        env:
          ACTION: 'print'
```

## Scheduling and best practices

One option to scale the Ruler is by scaling it horizontally. However, with multiple Ruler instances running they will need to coordinate to determine which instance will evaluate which rule. Similar to the ingesters, the Rulers establish a hash ring to divide up the responsibilities of evaluating rules.

The possible configurations are listed fully in the [configuration documentation]({{< relref "../configure" >}}), but in order to shard rules across multiple Rulers, the rules API must be enabled via flag (`-ruler.enable-api`) or config file parameter. Secondly, the Ruler requires its own ring to be configured. From there the Rulers will shard and handle the division of rules automatically. Unlike ingesters, Rulers do not hand over responsibility: all rules are re-sharded randomly every time a Ruler is added to or removed from the ring.

A full sharding-enabled Ruler example is:

```yaml
ruler:
    alertmanager_url: <alertmanager_endpoint>
    enable_alertmanager_v2: true
    enable_api: true
    enable_sharding: true
    ring:
        kvstore:
            consul:
                host: consul.loki-dev.svc.cluster.local:8500
            store: consul
    rule_path: /tmp/rules
    storage:
        gcs:
            bucket_name: <loki-rules-bucket>
```

## Ruler storage

The Ruler supports the following types of storage: `azure`, `gcs`, `s3`, `swift`, `cos` and `local`. Most kinds of storage work with the sharded Ruler configuration in an obvious way, that is, configure all Rulers to use the same backend.

The local implementation reads the rule files off of the local filesystem. This is a read-only backend that does not support the creation and deletion of rules through the [Ruler API]({{< relref "../reference/api#ruler" >}}). Despite the fact that it reads the local filesystem this method can still be used in a sharded Ruler configuration if the operator takes care to load the same rules to every Ruler. For instance, this could be accomplished by mounting a [Kubernetes ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/) onto every Ruler pod.

A typical local configuration might look something like:
```
  -ruler.storage.type=local
  -ruler.storage.local.directory=/tmp/loki/rules
```

With the above configuration, the Ruler would expect the following layout:
```
/tmp/loki/rules/<tenant id>/rules1.yaml
                           /rules2.yaml
```
Yaml files are expected to be [Prometheus-compatible](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/) but include LogQL expressions as specified in the beginning of this doc.

## Future improvements

There are a few things coming to increase the robustness of this service. In no particular order:

- WAL for recording rule.
- Backend metric stores adapters for generated alert rule data.

## Misc Details: Metrics backends vs in-memory

Currently the Loki Ruler is decoupled from a backing Prometheus store. Generally, the result of evaluating rules as well as the history of the alert's state are stored as a time series. Loki is unable to store/retrieve these in order to allow it to run independently of i.e. Prometheus. As a workaround, Loki keeps a small in memory store whose purpose is to lazy load past evaluations when rescheduling or resharding Rulers. In the future, Loki will support optional metrics backends, allowing storage of these metrics for auditing & performance benefits.
