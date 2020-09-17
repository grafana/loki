---
title: Alerting
weight: 700
---

# Alerting

Loki includes a component called the Ruler, adapted from our upstream project, Cortex. The Ruler is responsible for continually evaluating a set of configurable queries and then alerting when certain conditions happen, like the when they detect a high percentage of error logs.

## Prometheus Compatible

When running the ruler (by default in single binary form), Loki will accept rules files and then schedule them for continual evaluation. These are _Prometheus Compabile_! This means the rules file has the same structure as in [Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/), with the exception that the rules specified are in LogQL. Let's see what that looks like:

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
[ interval: <duration> | default = ruler.evaluation_interval || 1m ]

rules:
  [ - <rule> ... ]
```

### `<rule>`

The syntax for alerting rules is (see the LogQL [docs](https://grafana.com/docs/loki/latest/logql/#metric-queries) for more details):

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

A full fledged example of a rules file may look like:

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

## Use Cases

The Ruler's Prometheus compatibility further accentuates the marriage between metrics & logs. For those looking to get started alerting based on logs, or wondering why this may be useful, here are a few use cases we think fit very well.

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

### Black Box Monitoring

We don't always control the source code of applications we run. Think load balancers & the myriad components (both open source & closed 3rd party) that support our applications; it's a common problem that these don't expose a metric you want (or any metrics at all). How then, can we bring them into our observability stack in order to monitor them effectively? Alerting based on logs is a great answer for these problems.

For a sneak peek of how to combine this with the upcoming LogQL v2 functionality, take a look at Ward's [video](https://www.youtube.com/watch?v=RwQlR3D4Km4) which builds a robust nginx monitoring dashboard entirely from nginx logs.

### Event Alerting

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

### Alerting on High Cardinality Sources

Another great use case is alerting on high cardinality sources. These are things which are difficult/expensive to record as metrics because the potential label set is huge. A great example of this is per-tenant alerting in multi-tenanted systems like Loki. It's a common balancing act between the desire to have per-tenant metrics and the cardinality explosion that ensues (adding a single _tenant_ label to an existing Prometheus metric would increase it's cardinality by the number of tenants).

Creating these alerts in LogQL is attractive because these metrics can be extracted at _query time_, meaning we don't suffer the cardinality explosion in our metrics store.

Note: to really take advantage of this, we'll need some features from the upcoming LogQL v2 language. Stay tuned.

## Interacting with the Ruler

- interaction with cortextool (https://grafana.com/docs/grafana-cloud/alerts/alerts-rules/)
- github action for CI

## Scheduling & Best Practices

## Scaling

- (https://cortexmetrics.io/docs/guides/ruler-sharding/)

## Future Improvements

### Metrics backends vs in-memory


- prometheus compatiblle
- in memory series
- horizontally scalable, highly available
 
- changelog
- config docs
- future improvements
  - logqlv2!
  - metric backends: cortex
  - recording rules



