---
title: Metric queries 
menuTitle:
description: Provides an overview of how metric queries are constructed and parsed. Metric queries extend log queries by applying a function to log query results.
aliases: 
- ../logql/metric_queries/
weight: 400
---

# Metric queries

{{< shared id="metric-queries" >}}

Metric queries in LogQL let you analyze logs by applying mathematical operations to log query results. This feature allows you to create metrics from logs, such as calculating error rates or identifying the top log sources over a specific time period.

By using aggregation operators and functions, you can convert a set of log entries into a single numeric value or a time series. This helps you compute totals, rates, and trends based on either the number of log lines or a label. If you use a parser, you can calculate metrics using values from inside the log lines. You can use any label, including indexed or parsed labels, to create aggregations and generate new series.

{{< /shared >}}

For example, you can use metric queries to answer the following questions:

- Identify spikes or drops in activity: How many log lines per second are generated? How many error messages are generated per second?
- Identify the most frequently called endpoints: Which is the most accessed route in your API?
- Measure database performance: What is the 95th percentile of query execution time in your database?

## Key concepts

Before you start with metric queries, review these key terms:

- **Aggregation**: The process of combining multiple values into a single value. For example, summing all log entries over a time period is an aggregation.
- **Function**: A predefined operation that processes data and returns a result. For example, `sum()` adds up values, and `avg()` calculates their average.
- **Operator**: A symbol or keyword used to perform operations on data. For example, `+` is an arithmetic operator, and `by` is a grouping operator in LogQL.
- **Range vector aggregation** is a process in LogQL that allows you to analyze log data over a specific time range by applying mathematical functions to it.

And the Loki data types, which use the same concepts as [Prometheus data types](https://prometheus.io/docs/prometheus/latest/querying/basics/#expression-language-data-types):

- **Instant vector** - A selection of one or more numbers for a specified timestamp that can be used to generate a metric. This is typically represented as a single number or set of numbers.
  - For example: `count{cluster="prod", app="pizza"} \= 100
    count{cluster="prod", app="shipping"} \= 150`
- **Range vector** - A set of time series containing a range of data points over time for each time series. This is basically a set of values over time, which is typically represented as a graph.
  - A **range vector selector** is a way to specify a set of log entries or data points over a specific time range in LogQL. Think of it as a tool that lets you "zoom in" on a particular time period to analyze logs or metrics.
- **Scalar** - A simple numeric floating point value.
- **String** - A simple string value; currently not used in Loki metric queries. <!-- From previous docs. Check if string is used or not in Loki -->

## Analyze data over time

LogQL uses the concept of **range vectors**, which represent a range of log or label values over a specific time duration. Aggregation functions are applied to these range vectors to compute metrics.

There are two types of range vector aggregations:

1. **Log Range Aggregations**: Operate directly on log entries.
2. **Unwrapped Range Aggregations**: Use extracted labels as numerical values for calculations.

### Count logs over time

**Log range aggregations** apply functions to log entries over a specified time range to produce a single metric value. They analyze your logs over a set of time windows (ranges) to calculate a statistic.

The syntax for a log range aggregation is:

`range_function(log-range)`

A log range query consists of a query, followed by a duration, with a range function applied to aggregate the query over the duration. The duration can be placed after the log stream selector, or at the end of the query.

For example, a basic log range aggregation query looks like this:

`count_over_time({app="foo"}[1m])`

where:

- **count_over_time** is a range function that counts the number of log entries within each time interval.
- **{app="foo"}** is a Loki label selector that filters which logs to be included in the metric calculation.
- **[1m]** is the range that specifies how far back in time to look when calculating each data point.

#### Log range functions

You can use the following range functions in a log range aggregation metric query:

- **`absent_over_time(log-range)`**: Checks if no log entries exist for a given range. This can be particularly useful to create alerts for when no log stream exists for a given label combination over a time range.
- **`bytes_over_time(log-range)`**: Counts the total bytes used by each log stream for a given range.
- **`bytes_rate(log-range)`**: Calculates the number of bytes per second for each stream.
- **`count_over_time(log-range)`**: Counts the entries for each log stream within the given range.
- **`rate(log-range)`**: Calculates the number of log entries per second.

#### Example queries using log range aggregation

`count_over_time({service_name="mysql"}[5m])`

1. When run as an _instant_ query, counts all the log lines within the last five minutes for the MySQL job.
1. When run as a _range_ query in Grafana, executes the query over the selected time period and generates a trend graph where each data point shows the count of log lines over the previous five minute interval.

`sum by (host) (rate({service_name="mysql"} |= "error" != "timeout" | json | duration > 10s [1m]))`

1. When run as an _instant_ query, calculates the per-second rate of non-timeout errors for the `mysql` service over the last 1 minute, where the "duration" field in each log line is greater than 10 seconds, aggregated by `host`. The result is a single data point per host.
1. When run as a _range_ query in Grafana, calculates the per-second rate of errors over the selected time range, and generates a graph where each data point shows the rate over the previous 1 minute interval, aggregated by `host`. The result is a time series per host, which shows how error rates have changed over time.

#### Shift the time range of a query

The **offset modifier** lets you shift the time range of a query. You can use the offset modifier to "go back in time" and focus on logs from an earlier period. For example, if you want to analyze logs from 10 to 5 minutes ago instead of the last 5 minutes. This feature is particularly useful if you often have late-arriving logs and you want to ensure all logs are counted in a particular metric.

When using an `offset` modifier, the syntax is that the modifier follows the range vector selector, as follows:

`aggregation_function({<label-selector>}[<time>] offset <time>)`

For example, the following expression counts the number of logs from MySQL, for the time period 10 minutes ago to 5 minutes ago:

```logql
count_over_time({job="mysql"}[5m] offset 5m) // VALID SYNTAX

count_over_time({job="mysql"}[5m]) offset 5m // INVALID SYNTAX
```

### Extract values from logs

An **unwrapped range aggregation** operates on raw sample values either in index labels, or extracted from inside the log line content. This lets you produce a time series based on some value inside each log line, or from a property of each log line. You might use an unwrapped range aggregation to produce a time series from data contained in your log lines, such as:

- duration of a request to your service
- total bytes served in an HTTP request
- throughput (bytes/sec) of a request
- purchase price of an item
- transaction duration
- error or success status codes returned by a service

The syntax for an unwrapped range aggregation is as follows; where `parser` specifies a parser (such as `json`, `logfmt`) which produces a label that is used in the following `unwrap` expression:

`<aggregation-function>({<label-selector>} | <parser> | unwrap <label> | __error__='' [<range>])`

<!-- Original section.
### Unwrapped range aggregations
Unwrapped ranges use _extracted_ labels as sample values instead of log lines. However to select which label will be used within the aggregation, the log query must end with an unwrap expression and optionally a label filter expression to discard [errors](./#pipeline-errors).

The unwrap expression is noted `| unwrap label_identifier` where the label identifier is the label name to use for extracting sample values.

Since label values are strings, by default a conversion into a float (64bits) will be attempted, in case of failure the `__error__` label is added to the sample.
Optionally the label identifier can be wrapped by a conversion function `| unwrap <function>(label_identifier)`, which will attempt to convert the label value from a specific format.  -->

#### Supported Range Functions

Supported functions for operating over unwrapped ranges include:

- **`absent_over_time(unwrapped-range)`**: returns an empty vector if the range vector passed to it has any elements and a 1-element vector with the value 1 if the range vector passed to it has no elements. (`absent_over_time` is useful for alerting on when no time series and logs stream exist for label combination for a certain amount of time.)
- **`avg_over_time(unwrapped-range)`**: Calculates the average value.
- **`first_over_time(unwrapped-range)`**: The first value of all points in the specified interval.
- **`last_over_time(unwrapped-range)`**: The last value of all points in the specified interval.
- **`max_over_time(unwrapped-range)`**: Finds the maximum value.
- **`min_over_time(unwrapped-range)`**: Finds the minimum value.
- **`quantile_over_time(scalar, unwrapped-range)`**: Finds the φ-quantile (0 ≤ φ ≤ 1) (for example, median).
- **`rate(unwrapped-range)`**: Calculates the per-second rate of values.
- **`rate_counter(unwrapped-range)`**: Calculates per second rate of the values in the specified interval and treats them as a "counter metric".
- **`stddev_over_time(unwrapped-range)`**: Calculates the standard deviation.
- **`stdvar_over_time(unwrapped-range)`**: The population standard variance of the values in the specified interval.
- **`sum_over_time(unwrapped-range)`**: Sums all values over a time range.

Except for `absent_over_time`, `rate`, `rate_counter`, and `sum_over_time`, unwrapped range aggregations support grouping.

Additionally, the following **conversion functions** support interpreting text strings as numerical values:

- **`duration_seconds(label_identifier)`** (or its short equivalent `duration`) which will convert the label value in seconds from the [go duration format](https://golang.org/pkg/time/#ParseDuration) (for example, `5m`, `24s30ms`).
- **`bytes(label_identifier)`** which will convert the label value to raw bytes, assuming the value has one of the following size indicators: B, kB, MB, GB, TB, PB, KB, KiB, MiB, GiB, TiB, PiB.

#### Example unwrapped range aggregation queries

Example: With JSON log lines that look like this:

`{"http.request.method":"GET","http.request.duration":10,"http.request.headers.x-geo-country":"United States","client.address":"1.2.3.4"}`

You can find the average request duration by country for requests to the frontend, over 1 minute intervals, using the function `avg_over_time`:

`avg_over_time(
  {container="frontendproxy"} | json
    | unwrap http_request_duration [1m]
) by (http_request_headers_x_geo_country)`

You can find the 95th percentile of request duration by country for requests to the frontend, over 1 minute intervals, using the function `quantile_over_time`:

`quantile_over_time(0.95,
  {container="frontendproxy"} | json
    | unwrap http_request_duration [1m]
  ) by (http_request_headers_x_geo_country)`

Extract the sum of request sizes (in bytes) over the last 10 minutes:

`sum_over_time({job="web"} | unwrap bytes(request_size) [10m])`

## Combine or summarize data

When you write metric queries, you generally want to use some sort of aggregation to combine the data for a specific label or set of labels to generate your metric. If the labels have high cardinality, you may want to group the labels in some way to reduce the cardinality. For example, if you are parsing log lines containing IP addresses, without a grouping expression, your metric query would potentially produce a very large number of series. This will help you avoid generating a "maximum of series (500) reached for a single query" error.

Without applying an aggregation, Loki will return a time series for each possible label combination returned from your query and after any parsers are applied. Applying an aggregation produces a single set of time series for the label(s) that you are interested in. For example:

- sum by `hostname`
- sum by `region`
- sum by `pod` and `endpoint`

`sum` is the most commonly-used aggregation but you may also use any of the other **aggregation operators** to group and process data:

- **`avg`**: Calculates the average over labels.
- **`count`**: Counts the number of elements in the vector.
- **`max`**: Finds the maximum value.
- **`min`**: Finds the minimum value.
- **`sort`**: Sorts values in ascending order.
- **`sort_desc`**: Sorts values in descending order.
- **`stddev`**: Calculate the population standard deviation over labels.
- **`stdvar`**: Calculate the population standard variance over labels.
- **`sum`**: Adds up values.

There are two additional operators, `topk` and `bottomk` which differ from the other aggregators, in that only a subset of the input samples, including the original labels, are returned in the result vector.

- **`bottomk`**: Selects the smallest `k` elements by value. For example, the smallest five values.
- **`topk`**: Selects the top `k` elements by value. For example, the top ten values.

### Example queries using aggregation operators

1. Calculate the total number of log entries grouped by `host`:

```logql
sum by (host) (count_over_time({job="mysql"}[5m]))
```

1. Find the top three hosts with the highest log entry counts:

```logql
topk(3, sum by (host) (count_over_time({job="mysql"}[5m])))
```

### Grouping with `by` and `without`

There are two aggregation clauses, `by` and `without`. Each clause removes series from the result, but in different ways:

- **`by`**: Groups results by specific labels.
- **`without`**: Excludes specific labels from the grouping.

`by` and `without` are only used to group the input vector.

The syntax is as follows:

```logql
<aggr-op>([parameter,] <vector expression\>) [without|by (<label list>)]
```

#### Example queries using `by` and `without`

Find the average request duration for requests to the frontend, grouped by all labels _except_ the parsed label `client_address` (which in this case may be an IP address, and therefore high cardinality), over 1 minute intervals:

```logql
avg_over_time(
  {container="frontendproxy"} | json
  | unwrap http_request_duration [1m]
) without (client_address)
```

Find the average request duration for requests to the frontend, grouped by the `http_request_user_agent` parsed label only:

```logql
avg_over_time(
  {container="frontendproxy"} | json
  | unwrap http_request_duration [1m]
) by (http_request_user_agent)
```

## General purpose functions

LogQL also includes general-purpose functions:

- **`vector(s scalar)`**: Converts the scalar `s` value into a vector with no labels. This behaves identically to the [Prometheus `vector()` function](https://prometheus.io/docs/prometheus/latest/querying/functions/#vector). `vector` is mainly used to return a value for a series that would otherwise return nothing; this can be useful when using LogQL to define an alert.

**Example Query**

Count all the log lines within the last five minutes for the traefik namespace. Return `0` if no logs exist for the `traefik` namespace in the last 5 minutes:

```logql
sum(count_over_time({namespace="traefik"}[5m])) or vector(0)
```

## Probabilistic aggregation

{{< admonition type="note" >}}
Probabilistic aggregation is an experimental feature. Engineering and on-call support is not available. Documentation is either limited or not provided outside of code comments. No SLA is provided. To use this feature, set `limits_config.shard_aggregations:approx_topk` in your [Loki configuration](https://grafana.com/docs/loki/\<LOKI_VERSION\>/configure/#limits_config). To enable this feature in Grafana Cloud, contact Grafana Support.
{{\< admonition >}}

**`approx_topk`** is a function in LogQL that helps you find the most frequent values in a dataset when working with very large amounts of log data. It provides an _approximate_ result instead of an exact one, which makes it faster and more efficient for queries that might otherwise take too long or fail due to system limits.

For example, if you want to find the top 5 most common error codes in your logs but have millions of log entries, `approx_topk` can quickly give you a good estimate of the top results without needing to process every single log entry.

### Key Points

- **Purpose**: It’s used as a faster alternative to the `topk` function when dealing with large datasets.
- **Syntax**:

```logql
approx_topk(k, <vector expression>)
```

- `k`: The number of top results you want (e.g., top 5 or top 10).
- `<vector expression>`: The data you want to analyze.
- **Use Case**: Ideal for situations where speed is more important than exact accuracy, such as dashboards or exploratory queries.
- **Limitations**:
- Only works with **instant queries** (queries that return a single result for a specific point in time).
- Does not support grouping directly; you need to use an inner `sum by` or `sum without` to group data before applying `approx_topk`.

**Example Query**

Find the top three most frequent log sources in your dataset:

```logql
approx_topk(3, sum by (source) (rate({job="web"}[5m])))
```

This query approximates the top three sources of logs in the last 5 minutes.

### How It Works

Under the hood, `approx_topk` uses a mathematical technique called the **count-min sketch** algorithm. This algorithm efficiently estimates the frequency of items in a dataset by dividing the data into smaller "shards" and processing them separately. The results are then combined to produce the final approximation.

In summary, `approx_topk` is a powerful tool for quickly identifying trends or patterns in large log datasets when exact precision is not required.

## Further resources

- Watch: [How to turn logs into metrics with Grafana Loki](https://youtube.com/live/tKcnQ0Q2E-k) (Loki Community Call July 2025)
