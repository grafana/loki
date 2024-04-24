---
title: metrics
menuTitle:  
description: The 'metrics' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/metrics/
weight:  
---

# metrics

The `metrics` stage is an action stage that allows for defining and updating
metrics based on data from the extracted map. Note that created metrics are not
pushed to Loki and are instead exposed via Promtail's `/metrics` endpoint.
Prometheus should be configured to scrape Promtail to be able to retrieve the
metrics configured by this stage. If Promtail's configuration is reloaded, 
all metrics will be reset.

## Schema

```yaml
# A map where the key is the name of the metric and the value is a specific
# metric type.
metrics:
  [<string>: [ <metric_counter> | <metric_gauge> | <metric_histogram> ] ...]
```

### metric_counter

Defines a counter metric whose value only goes up.

```yaml
# The metric type. Must be Counter.
type: Counter

# Describes the metric.
[description: <string>]

# Defines custom prefix name for the metric. If undefined, default name "promtail_custom_" will be prefixed.
[prefix: <string>]

# Key from the extracted data map to use for the metric,
# defaulting to the metric's name if not present.
[source: <string>]

# Label values on metrics are dynamic which can cause exported metrics
# to go stale (for example when a stream stops receiving logs).
# To prevent unbounded growth of the /metrics endpoint any metrics which
# have not been updated within this time will be removed.
# Must be greater than or equal to '1s', if undefined default is '5m'
[max_idle_duration: <string>]

config:
  # If present and true all log lines will be counted without
  # attempting to match the source to the extract map.
  # It is an error to specify `match_all: true` and also specify a `value`
  [match_all: <bool>]

  # If present and true all log line bytes will be counted.
  # It is an error to specify `count_entry_bytes: true` without specifying `match_all: true`
  # It is an error to specify `count_entry_bytes: true` without specifying `action: add`
  [count_entry_bytes: <bool>]

  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "inc" or "add" (case insensitive). If
  # inc is chosen, the metric value will increase by 1 for each
  # log line received that passed the filter. If add is chosen,
  # the extracted value must be convertible to a positive float
  # and its value will be added to the metric.
  action: <string>
```

### metric_gauge

Defines a gauge metric whose value can go up or down.

```yaml
# The metric type. Must be Gauge.
type: Gauge

# Describes the metric.
[description: <string>]

# Defines custom prefix name for the metric. If undefined, default name "promtail_custom_" will be prefixed.
[prefix: <string>]

# Key from the extracted data map to use for the metric,
# defaulting to the metric's name if not present.
[source: <string>]

# Label values on metrics are dynamic which can cause exported metrics
# to go stale (for example when a stream stops receiving logs).
# To prevent unbounded growth of the /metrics endpoint any metrics which
# have not been updated within this time will be removed.
# Must be greater than or equal to '1s', if undefined default is '5m'
[max_idle_duration: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "set", "inc", "dec"," add", or "sub". If
  # add, set, or sub is chosen, the extracted value must be
  # convertible to a positive float. inc and dec will increment
  # or decrement the metric's value by 1 respectively.
  action: <string>
```

### metric_histogram

Defines a histogram metric whose values are bucketed.

```yaml
# The metric type. Must be Histogram.
type: Histogram

# Describes the metric.
[description: <string>]

# Defines custom prefix name for the metric. If undefined, default name "promtail_custom_" will be prefixed.
[prefix: <string>]

# Key from the extracted data map to use for the metric,
# defaulting to the metric's name if not present.
[source: <string>]

# Label values on metrics are dynamic which can cause exported metrics
# to go stale (for example when a stream stops receiving logs).
# To prevent unbounded growth of the /metrics endpoint any metrics which
# have not been updated within this time will be removed.
# Must be greater than or equal to '1s', if undefined default is '5m'
[max_idle_duration: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Holds all the numbers in which to bucket the metric.
  buckets:
    - <int>
```

## Examples

### Counter

```yaml
- metrics:
    log_lines_total:
      type: Counter
      description: "total number of log lines"
      prefix: my_promtail_custom_
      max_idle_duration: 24h
      config:
        match_all: true
        action: inc
    log_bytes_total:
      type: Counter
      description: "total bytes of log lines"
      prefix: my_promtail_custom_
      max_idle_duration: 24h
      config:
        match_all: true
        count_entry_bytes: true
        action: add
```

This pipeline creates a `log_lines_total` counter which increments for every log line received
by using the `match_all: true` parameter.

It also creates a `log_bytes_total` counter which adds the byte size of every log line received
to the counter by using the `count_entry_bytes: true` parameter.

Those two metrics will disappear after 24h if they don't receive new entries, this is useful to reduce the building up of stage metrics.

The combination of these two metric stages will give you two counters to track the volume of
every log stream in both number of lines and bytes, which can be useful in identifying sources
of very high volume, as well as helping understand why you may have too much cardinality.

These stages should be placed towards the end of your pipeline after any `labels` stages

```yaml
- regex:
    expression: "^.*(?P<order_success>order successful).*$"
- metrics:
    successful_orders_total:
      type: Counter
      description: "log lines with the message `order successful`"
      source: order_success
      config:
        action: inc
```

This pipeline first tries to find `order successful` in the log line, extracting
it as the `order_success` field in the extracted map. The metrics stage then
creates a metric called `successful_orders_total` whose value only increases when
`order_success` was found in the extracted map.

The result of this pipeline is a metric whose value only increases when a log
line with the text `order successful` was scraped by Promtail.

```yaml
- regex:
    expression: "^.* order_status=(?P<order_status>.*?) .*$"
- metrics:
    successful_orders_total:
      type: Counter
      description: "successful orders"
      source: order_status
      config:
        value: success
        action: inc
    failed_orders_total:
      type: Counter
      description: "failed orders"
      source: order_status
      config:
        value: fail
        action: inc
```

This pipeline first tries to find text in the format `order_status=<value>` in
the log line, pulling out the `<value>` into the extracted map with the key
`order_status`.

The metric stages creates `successful_orders_total` and `failed_orders_total`
metrics that only increment when the value of `order_status` in the extracted
map is `success` or `fail` respectively.

### Gauge

Gauge examples will be very similar to Counter examples with additional `action`
values.

```yaml
- regex:
    expression: "^.* retries=(?P<retries>\d+) .*$"
- metrics:
    retries_total:
      type: Gauge
      description: "total retries"
      source: retries
      config:
        action: add
```

This pipeline first tries to find text in the format `retries=<value>` in the
log line, pulling out the `<value>` into the extracted map with the key
`retries`. Note that the regex only parses numbers for the value in `retries`.

The metrics stage then creates a Gauge whose current value will be added to the
number in the `retries` field from the extracted map.

### Histogram

```yaml
- metrics:
    http_response_time_seconds:
      type: Histogram
      description: "length of each log line"
      source: response_time
      config:
        buckets: [0.001,0.0025,0.005,0.010,0.025,0.050]
```

This pipeline creates a histogram that reads `response_time` from the extracted
map and places it into a bucket, both increasing the count of the bucket and the
sum for that particular bucket.

## Supported values

The metric values extracted from the log data are internally converted to floating points. 
The supported values are the following:
* integers, floating point numbers
* string - two types of string formats are supported:
  * strings that represent floating point numbers: e.g., `"0.804"` is converted to `0.804`.
  * duration format strings. Valid time units are "ns", "us", "ms", "s", "m", "h". 
    Values in this format are converted as a floating point number of seconds. E.g., `"0.5ms"` is converted to `0.0005`.
* boolean:
  * `true` is converted to `1`
  * `false` is converted to `0`
