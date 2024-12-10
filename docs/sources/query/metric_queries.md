---
title: Metric queries 
menuTItle:  
description: Provides an overview of how metric queries are constructed and parsed. Metric queries extend log queries by applying a function to log query results.
aliases: 
- ../logql/metric_queries/
weight: 400  
---

# Metric queries

Metric queries extend log queries by applying a function to log query results.
This powerful feature creates metrics from logs.

Metric queries can be used to calculate the rate of error messages or the top N log sources with the greatest quantity of logs over the last 3 hours.

Combined with parsers, metric queries can also be used to calculate metrics from a sample value within the log line, such as latency or request size.
All labels, including extracted ones, will be available for aggregations and generation of new series.

## Range Vector aggregation

LogQL shares the [range vector](https://prometheus.io/docs/prometheus/latest/querying/basics/#range-vector-selectors) concept of Prometheus.
In Grafana Loki, the selected range of samples is a range of selected log or label values.

The aggregation is applied over a time duration.
Loki defines [Time Durations](https://prometheus.io/docs/prometheus/latest/querying/basics/#time-durations) with the same syntax as Prometheus.

Loki supports two types of range vector aggregations: log range aggregations and unwrapped range aggregations.

### Log range aggregations

A log range aggregation is a query followed by a duration.
A function is applied to aggregate the query over the duration.
The duration can be placed 
after the log stream selector or at end of the log pipeline.

The functions:

- `rate(log-range)`: calculates the number of entries per second
- `count_over_time(log-range)`: counts the entries for each log stream within the given range.
- `bytes_rate(log-range)`: calculates the number of bytes per second for each stream.
- `bytes_over_time(log-range)`: counts the amount of bytes used by each log stream for a given range.
- `absent_over_time(log-range)`: returns an empty vector if the range vector passed to it has any elements and a 1-element vector with the value 1 if the range vector passed to it has no elements. (`absent_over_time` is useful for alerting on when no time series and logs stream exist for label combination for a certain amount of time.)

Examples:

- Count all the log lines within the last five minutes for the MySQL job.

    ```logql
    count_over_time({job="mysql"}[5m])
    ```

- This aggregation includes filters and parsers.
    It returns the per-second rate of all non-timeout errors within the last minutes per host for the MySQL job and only includes errors whose duration is above ten seconds.

    ```logql
    sum by (host) (rate({job="mysql"} |= "error" != "timeout" | json | duration > 10s [1m]))
    ```

#### Offset modifier
The offset modifier allows changing the time offset for individual range vectors in a query.

For example, the following expression counts all the logs within the last ten minutes to five minutes rather than last five minutes for the MySQL job. Note that the `offset` modifier always needs to follow the range vector selector immediately.
```logql
count_over_time({job="mysql"}[5m] offset 5m) // GOOD
count_over_time({job="mysql"}[5m]) offset 5m // INVALID
```

### Unwrapped range aggregations

Unwrapped ranges uses extracted labels as sample values instead of log lines. However to select which label will be used within the aggregation, the log query must end with an unwrap expression and optionally a label filter expression to discard [errors]({{< relref ".#pipeline-errors" >}}).

The unwrap expression is noted `| unwrap label_identifier` where the label identifier is the label name to use for extracting sample values.

Since label values are string, by default a conversion into a float (64bits) will be attempted, in case of failure the `__error__` label is added to the sample.
Optionally the label identifier can be wrapped by a conversion function `| unwrap <function>(label_identifier)`, which will attempt to convert the label value from a specific format.

We currently support the functions:
- `duration_seconds(label_identifier)` (or its short equivalent `duration`) which will convert the label value in seconds from the [go duration format](https://golang.org/pkg/time/#ParseDuration) (e.g `5m`, `24s30ms`).
- `bytes(label_identifier)` which will convert the label value to raw bytes applying the bytes unit  (e.g. `5 MiB`, `3k`, `1G`).

Supported function for operating over unwrapped ranges are:

- `rate(unwrapped-range)`: calculates per second rate of the sum of all values in the specified interval.
- `rate_counter(unwrapped-range)`: calculates per second rate of the values in the specified interval and treating them as "counter metric"
- `sum_over_time(unwrapped-range)`: the sum of all values in the specified interval.
- `avg_over_time(unwrapped-range)`: the average value of all points in the specified interval.
- `max_over_time(unwrapped-range)`: the maximum value of all points in the specified interval.
- `min_over_time(unwrapped-range)`: the minimum value of all points in the specified interval
- `first_over_time(unwrapped-range)`: the first value of all points in the specified interval
- `last_over_time(unwrapped-range)`: the last value of all points in the specified interval
- `stdvar_over_time(unwrapped-range)`: the population standard variance of the values in the specified interval.
- `stddev_over_time(unwrapped-range)`: the population standard deviation of the values in the specified interval.
- `quantile_over_time(scalar,unwrapped-range)`: the φ-quantile (0 ≤ φ ≤ 1) of the values in the specified interval.
- `absent_over_time(unwrapped-range)`: returns an empty vector if the range vector passed to it has any elements and a 1-element vector with the value 1 if the range vector passed to it has no elements. (`absent_over_time` is useful for alerting on when no time series and logs stream exist for label combination for a certain amount of time.)

Except for `sum_over_time`,`absent_over_time`, `rate` and `rate_counter`, unwrapped range aggregations support grouping.

```logql
<aggr-op>([parameter,] <unwrapped-range>) [without|by (<label list>)]
```

Which can be used to aggregate over distinct labels dimensions by including a `without` or `by` clause.

`without` removes the listed labels from the result vector, while all other labels are preserved the output. `by` does the opposite and drops labels that are not listed in the `by` clause, even if their label values are identical between all elements of the vector.

See [Unwrap examples]({{< relref "./query_examples#unwrap-examples" >}}) for query examples that use the unwrap expression.

## Built-in aggregation operators

Like [PromQL](https://prometheus.io/docs/prometheus/latest/querying/operators/#aggregation-operators), LogQL supports a subset of built-in aggregation operators that can be used to aggregate the element of a single vector, resulting in a new vector of fewer elements but with aggregated values:

- `sum`: Calculate sum over labels
- `avg`: Calculate the average over labels
- `min`: Select minimum over labels
- `max`: Select maximum over labels
- `stddev`: Calculate the population standard deviation over labels
- `stdvar`: Calculate the population standard variance over labels
- `count`: Count number of elements in the vector
- `topk`: Select largest k elements by sample value
- `bottomk`: Select smallest k elements by sample value
- `sort`: returns vector elements sorted by their sample values, in ascending order.
- `sort_desc`: Same as sort, but sorts in descending order.

The aggregation operators can either be used to aggregate over all label values or a set of distinct label values by including a `without` or a `by` clause:

```logql
<aggr-op>([parameter,] <vector expression>) [without|by (<label list>)]
```

`parameter` is required when using `topk` and `bottomk`.
`topk` and `bottomk` are different from other aggregators in that a subset of the input samples, including the original labels, are returned in the result vector.

`by` and `without` are only used to group the input vector.
The `without` clause removes the listed labels from the resulting vector, keeping all others.
The `by` clause does the opposite, dropping labels that are not listed in the clause, even if their label values are identical between all elements of the vector.

See [vector aggregation examples]({{< relref "./query_examples#vector-aggregation-examples" >}}) for query examples that use vector aggregation expressions.

## Functions

LogQL supports a set of built-in functions.

- `vector(s scalar)`: returns the scalar s as a vector with no labels. This behaves identically to the [Prometheus `vector()` function](https://prometheus.io/docs/prometheus/latest/querying/functions/#vector).
  `vector` is mainly used to return a value for a series that would otherwise return nothing; this can be useful when using LogQL to define an alert.

Examples:

- Count all the log lines within the last five minutes for the traefik namespace.

    ```logql
    sum(count_over_time({namespace="traefik"}[5m])) # will return nothing
      or
    vector(0) # will return 0
    ```

## Probabilistic aggregation

The `topk` keyword lets you find the largest 1,000 elements in a data stream by sample size. When  `topk` hits the maximum series limit, LogQL also supports using a probable approximation; `approx_topk`  is a drop-in replacement when `topk` hits the maximum series limit.

```logql
approx_topk(k, <vector expression>)
```

It is only supported for instant queries and does not support grouping. It is useful when the cardinality of the inner
vector is too high, for example, when it uses an aggregation by a structured metadata label.
