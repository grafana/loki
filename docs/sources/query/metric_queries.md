---
title: Metric queries 
menuTItle:  
description: Metric queries extend log queries by applying a function to log query results. This powerful feature creates metrics from logs.
aliases: 
- ../logql/metric_queries/
weight: 20  
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

### Unwrapped range aggregations

Unwrapped ranges uses extracted labels as sample values instead of log lines. However to select which label will be used within the aggregation, the log query must end with an unwrap expression and optionally a label filter expression to discard [errors]({{< relref "./log_queries#pipeline-errors" >}}).

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

>Metric queries cannot contain errors, in case errors are found during execution, Loki will return an error and appropriate status code.

If you need to remove errors from an unwrap expression, place a relevant label filter expression `__error__ = ""` after unwrap to remove all or specific errors.

```logql
quantile_over_time(
	0.99,
	{container="ingress-nginx",service="hosted-grafana"}
	| json
	| unwrap response_latency_seconds
	| __error__=""[1m]
	) by (cluster)
```

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

## Binary operators

### Arithmetic operators

The following binary arithmetic operators exist in Loki:

- `+` (addition)
- `-` (subtraction)
- `*` (multiplication)
- `/` (division)
- `%` (modulo)
- `^` (power/exponentiation)

Binary arithmetic operators are defined between two literals (scalars), a literal and a vector, and two vectors.

Between two literals, the behavior is obvious:
They evaluate to another literal that is the result of the operator applied to both scalar operands (`1 + 1 = 2`).

Between a vector and a literal, the operator is applied to the value of every data sample in the vector, e.g. if a time series vector is multiplied by 2, the result is another vector in which every sample value of the original vector is multiplied by 2.

Between two vectors, a binary arithmetic operator is applied to each entry in the left-hand side vector and its matching element in the right-hand vector.
The result is propagated into the result vector with the grouping labels becoming the output label set. Entries for which no matching entry in the right-hand vector can be found are not part of the result.

Pay special attention to [operator order](#order-of-operations) when chaining arithmetic operators.

#### Arithmetic Examples

Implement a health check with a simple query:

```logql
1 + 1
```

Double the rate of a log stream's entries:

```logql
sum(rate({app="foo"}[1m])) * 2
```

Get proportion of warning logs to error logs for the `foo` app

```logql
sum(rate({app="foo", level="warn"}[1m])) / sum(rate({app="foo", level="error"}[1m]))
```

### Logical and set operators

These logical/set binary operators are only defined between two vectors:

- `and` (intersection)
- `or` (union)
- `unless` (complement)

`vector1 and vector2` results in a vector consisting of the elements of vector1 for which there are elements in vector2 with exactly matching label sets.
Other elements are dropped.

`vector1 or vector2` results in a vector that contains all original elements (label sets + values) of vector1 and additionally all elements of vector2 which do not have matching label sets in vector1.

`vector1 unless vector2` results in a vector consisting of the elements of vector1 for which there are no elements in vector2 with exactly matching label sets.
All matching elements in both vectors are dropped.

##### Binary operators examples

This contrived query will return the intersection of these queries, effectively `rate({app="bar"})`:

```logql
rate({app=~"foo|bar"}[1m]) and rate({app="bar"}[1m])
```

### Comparison operators

- `==` (equality)
- `!=` (inequality)
- `>` (greater than)
- `>=` (greater than or equal to)
- `<` (less than)
- `<=` (less than or equal to)

Comparison operators are defined between scalar/scalar, vector/scalar, and vector/vector value pairs.
By default they filter.
Their behavior can be modified by providing `bool` after the operator, which will return 0 or 1 for the value rather than filtering.

Between two scalars, these operators result in another scalar that is either 0 (false) or 1 (true), depending on the comparison result.
The `bool` modifier must **not** be provided.

`1 >= 1` is equivalent to `1`

Between a vector and a scalar, these operators are applied to the value of every data sample in the vector, and vector elements between which the comparison result is false get dropped from the result vector.
If the `bool` modifier is provided, vector elements that would be dropped instead have the value 0 and vector elements that would be kept have the value 1.

Filters the streams which logged at least 10 lines in the last minute:

```logql
count_over_time({foo="bar"}[1m]) > 10
```

Attach the value(s) `0`/`1` to streams that logged less/more than 10 lines:

```logql
count_over_time({foo="bar"}[1m]) > bool 10
```

Between two vectors, these operators behave as a filter by default, applied to matching entries.
Vector elements for which the expression is not true or which do not find a match on the other side of the expression get dropped from the result, while the others are propagated into a result vector.
If the `bool` modifier is provided, vector elements that would have been dropped instead have the value 0 and vector elements that would be kept have the value 1, with the grouping labels again becoming the output label set.

Return the streams matching `app=foo` without app labels that have higher counts within the last minute than their counterparts matching `app=bar` without app labels:

```logql
sum without(app) (count_over_time({app="foo"}[1m])) > sum without(app) (count_over_time({app="bar"}[1m]))
```

Same as above, but vectors have their values set to `1` if they pass the comparison or `0` if they fail/would otherwise have been filtered out:

```logql
sum without(app) (count_over_time({app="foo"}[1m])) > bool sum without(app) (count_over_time({app="bar"}[1m]))
```

### Order of operations

When chaining or combining operators, you have to consider operator precedence:
Generally, you can assume regular [mathematical convention](https://en.wikipedia.org/wiki/Order_of_operations) with operators on the same precedence level being left-associative.

More details can be found in the [Golang language documentation](https://golang.org/ref/spec#Operator_precedence).

`1 + 2 / 3` is equal to `1 + ( 2 / 3 )`.

`2 * 3 % 2` is evaluated as `(2 * 3) % 2`.

### Keywords on and ignoring
The `ignoring` keyword causes specified labels to be ignored during matching.
The syntax:
```logql
<vector expr> <bin-op> ignoring(<labels>) <vector expr>
```
This example will return the machines which total count within the last minutes exceed average value for app `foo`.
```logql
max by(machine) (count_over_time({app="foo"}[1m])) > bool ignoring(machine) avg(count_over_time({app="foo"}[1m]))
```
The on keyword reduces the set of considered labels to a specified list.
The syntax:
```logql
<vector expr> <bin-op> on(<labels>) <vector expr>
```
This example will return every machine total count within the last minutes ratio in app `foo`:
```logql
sum by(machine) (count_over_time({app="foo"}[1m])) / on() sum(count_over_time({app="foo"}[1m]))
```

### Many-to-one and one-to-many vector matches
Many-to-one and one-to-many matchings occur when each vector element on the "one"-side can match with multiple elements on the "many"-side. You must explicitly request matching by using the group_left or group_right modifier, where left or right determines which vector has the higher cardinality.
The syntax:
```logql
<vector expr> <bin-op> ignoring(<labels>) group_left(<labels>) <vector expr>
<vector expr> <bin-op> ignoring(<labels>) group_right(<labels>) <vector expr>
<vector expr> <bin-op> on(<labels>) group_left(<labels>) <vector expr>
<vector expr> <bin-op> on(<labels>) group_right(<labels>) <vector expr>
```
The label list provided with the group modifier contains additional labels from the "one"-side that are included in the result metrics. And a label should only appear in one of the lists specified by `on` and `group_x`. Every time series of the result vector must be uniquely identifiable.
Grouping modifiers can only be used for comparison and arithmetic. By default, the system matches `and`, `unless`, and `or` operations with all entries in the right vector.

The following example returns the rates requests partitioned by `app` and `status` as a percentage of total requests.
```logql
sum by (app, status) (
  rate(
    {job="http-server"}
      | json
      [5m]
  )
)
/ on (app) group_left
sum by (app) (
  rate(
    {job="http-server"}
      | json
      [5m]
  )
)

=>
[
  {app="foo", status="200"} => 0.8
  {app="foo", status="400"} => 0.1
  {app="foo", status="500"} => 0.1
]
```
This version uses `group_left(<labels>)` to include `<labels>` from the right hand side in the result and returns the cost of discarded events per user, organization, and namespace:
```logql
sum by (user, namespace) (
  rate(
    {job="events"}
      | logfmt
      | discarded="true"
      [5m]
  )
)
* on (user) group_left(organization)
max_over_time(
  {job="cost-calculator"}
    | logfmt
    | unwrap cost
    [5m]
) by (user, organization)

=>
[
  {user="foo", namespace="dev", organization="little-org"} => 10
  {user="foo", namespace="prod", organization="little-org"} => 50
  {user="bar", namespace="dev", organization="big-org"} => 70
  {user="bar", namespace="prod", organization="big-org"} => 200
]
```

## Functions

LogQL supports a set of built-in functions.

### vector()

`vector(s scalar)`: returns the scalar s as a vector with no labels. This behaves identically to the [Prometheus `vector()` function](https://prometheus.io/docs/prometheus/latest/querying/functions/#vector).
`vector` is mainly used to return a value for a series that would otherwise return nothing; this can be useful when using LogQL to define an alert.

Examples:

Count all the log lines within the last five minutes for the traefik namespace.

```logql
sum(count_over_time({namespace="traefik"}[5m])) # will return nothing
  or
vector(0) # will return 0
```

### label_replace()

For each timeseries in `v`,

```
label_replace(v instant-vector,
    dst_label string,
    replacement string,
    src_label string,
    regex string)
```
matches the regular expression `regex` against the label `src_label`.
If it matches, then the timeseries is returned with the label `dst_label` replaced by the expansion of `replacement`.

`$1` is replaced with the first matching subgroup,
`$2` with the second etc.
If the regular expression doesn't match,
then the timeseries is returned unchanged.

This example will return a vector with each time series having a `foo` label with the value `a` added to it:

```logql
label_replace(rate({job="api-server",service="a:c"} |= "err" [1m]), "foo", "$1",
  "service", "(.*):.*")
```
