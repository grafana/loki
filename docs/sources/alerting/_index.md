---
title: Alerting
weight: 700
---

# Alerting

Loki includes a component called the Ruler, adapted from our upstream project, Cortex. The Ruler is responsible for continually evaluating a set of configurable queries and then alerting when certain conditions happen, e.g. a high percentage of error logs.

First, ensure the Ruler component is enabled. The following is a basic configuration which loads rules from configuration files:

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

## Prometheus Compatible

When running the Ruler (which runs by default in the single binary), Loki accepts rules files and then schedules them for continual evaluation. These are _Prometheus compatible_! This means the rules file has the same structure as in [Prometheus' Alerting Rules](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/), except that the rules specified are in LogQL.

Let's see what that looks like:

The syntax of a rule file is:

```yaml
groups:
  [ - <rule_group> ]
```

A simple example file could be:

```yaml
groups:
  - name: example
    rules:
    - alert: HighThroughputLogStreams
      expr: sum by(container) (rate({job=~"loki-dev/.*"}[1m])) > 1000
      for: 2m
```

### `<rule_group>`

```yaml
# The name of the group. Must be unique within a file.
name: <string>

# How often rules in the group are evaluated.
[ interval: <duration> | default = Ruler.evaluation_interval || 1m ]

rules:
  [ - <rule> ... ]
```

### `<rule>`

The syntax for alerting rules is (see the LogQL [Metric Queries](https://grafana.com/docs/loki/latest/logql/#metric-queries) for more details):

```yaml
# The name of the alert. Must be a valid label value.
alert: <string>

# The LogQL expression to evaluate (must be an instant vector). Every evaluation cycle this is
# evaluated at the current time, and all resultant time series become
# pending/firing alerts.
expr: <string>

# Alerts are considered firing once they have been returned for this long.
# Alerts which have not yet fired for long enough are considered pending.
[ for: <duration> | default = 0s ]

# Labels to add or overwrite for each alert.
labels:
  [ <labelname>: <tmpl_string> ]

# Annotations to add to each alert.
annotations:
  [ <labelname>: <tmpl_string> ]
```

### Example

A full-fledged example of a rules file might look like:

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

## Use cases

The Ruler's Prometheus compatibility further accentuates the marriage between metrics and logs. For those looking to get started alerting based on logs, or wondering why this might be useful, here are a few use cases we think fit very well.

### We aren't using metrics yet

Many nascent projects, apps, or even companies may not have a metrics backend yet. We tend to add logging support before metric support, so if you're in this stage, alerting based on logs can help bridge the gap. It's easy to start building Loki alerts for things like _the percentage of error logs_ such as the example from earlier:
```yaml
- alert: HighPercentageError
  expr: |
  sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)
    /
  sum(rate({app="foo", env="production"}[5m])) by (job)
    > 0.05
```

### Black box monitoring

We don't always control the source code of applications we run. Think load balancers and the myriad components (both open source and closed third-party) that support our applications; it's a common problem that these don't expose a metric you want (or any metrics at all). How then, can we bring them into our observability stack in order to monitor them effectively? Alerting based on logs is a great answer for these problems.

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

Another great use case is alerting on high cardinality sources. These are things which are difficult/expensive to record as metrics because the potential label set is huge. A great example of this is per-tenant alerting in multi-tenanted systems like Loki. It's a common balancing act between the desire to have per-tenant metrics and the cardinality explosion that ensues (adding a single _tenant_ label to an existing Prometheus metric would increase it's cardinality by the number of tenants).

Creating these alerts in LogQL is attractive because these metrics can be extracted at _query time_, meaning we don't suffer the cardinality explosion in our metrics store.

> **Note** As an example, we can use LogQL v2 to help Loki to monitor _itself_, alerting us when specific tenants have queries that take longer than 10s to complete! To do so, we'd use the following query: `sum by (org_id) (rate({job="loki-prod/query-frontend"} |= "metrics.go" | logfmt | duration > 10s [1m]))`

## Interacting with the Ruler

Because the rule files are identical to Prometheus rule files, we can interact with the Loki Ruler via [`cortex-tool`](https://github.com/grafana/cortex-tools#rules). The CLI is in early development, but works alongside both Loki and cortex. Make sure to pass the `--backend=loki` argument to commands when using it with Loki.

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

The possible configurations are listed fully in the [configuration documentation](https://grafana.com/docs/loki/latest/configuration/), but in order to shard rules across multiple Rulers, the rules API must be enabled via flag (`-experimental.Ruler.enable-api`) or config file parameter. Secondly, the Ruler requires it's own ring be configured. From there the Rulers will shard and handle the division of rules automatically. Unlike ingesters, Rulers do not hand over responsibility: all rules are re-sharded randomly every time a Ruler is added to or removed from the ring.

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

The Ruler supports six kinds of storage: configdb, azure, gcs, s3, swift, and local. Most kinds of storage work with the sharded Ruler configuration in an obvious way, i.e. configure all Rulers to use the same backend.

The local implementation reads the rule files off of the local filesystem. This is a read-only backend that does not support the creation and deletion of rules through the [Ruler API](https://grafana.com/docs/loki/latest/api/#Ruler). Despite the fact that it reads the local filesystem this method can still be used in a sharded Ruler configuration if the operator takes care to load the same rules to every Ruler. For instance, this could be accomplished by mounting a [Kubernetes ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/) onto every Ruler pod.

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
Yaml files are expected to be [Prometheus compatible](#Prometheus_Compatible) but include LogQL expressions as specified in the beginning of this doc.

## Future improvements

There are a few things coming to increase the robustness of this service. In no particular order:

- Recording rules.
- Backend metric stores adapters for generated alert and recording rule data. The first will likely be Cortex, as Loki is built atop it.

## Misc Details: Metrics backends vs in-memory

Currently the Loki Ruler is decoupled from a backing Prometheus store. Generally, the result of evaluating rules as well as the history of the alert's state are stored as a time series. Loki is unable to store/retrieve these in order to allow it to run independently of i.e. Prometheus. As a workaround, Loki keeps a small in memory store whose purpose is to lazy load past evaluations when rescheduling or resharding Rulers. In the future, Loki will support optional metrics backends, allowing storage of these metrics for auditing & performance benefits.
