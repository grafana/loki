# `metrics` stage

The `metrics` stage is an action stage that allows for defining and updating
metrics based on data from the extracted map. Note that created metrics are not
pushed to Loki and are instead exposed via Promtail's `/metrics` endpoint.
Prometheus should be configured to scrape Promtail to be able to retrieve the
metrics configured by this stage.

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

# Key from the extracted data map to use for the mtric,
# defaulting to the metric's name if not present.
[source: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "inc" or "add" (case insensitive). If
  # inc is chosen, the metric value will increase by 1 for each
  # log line receieved that passed the filter. If add is chosen,
  # the extracted value most be convertible to a positive float
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

# Key from the extracted data map to use for the mtric,
# defaulting to the metric's name if not present.
[source: <string>]

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

# Key from the extracted data map to use for the mtric,
# defaulting to the metric's name if not present.
[source: <string>]

config:
  # Filters down source data and only changes the metric
  # if the targeted value exactly matches the provided string.
  # If not present, all data will match.
  [value: <string>]

  # Must be either "inc" or "add" (case insensitive). If
  # inc is chosen, the metric value will increase by 1 for each
  # log line receieved that passed the filter. If add is chosen,
  # the extracted value most be convertible to a positive float
  # and its value will be added to the metric.
  action: <string>

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
      source: time
      config:
        action: inc
```

This pipeline creates a `log_lines_total` counter that increments whenever the
extracted map contains a key for `time`. Since every log entry has a timestamp,
this is a good field to use to count every line. Notice that `value` is not
defined in the `config` section as we want to count every line and don't need to
filter the value. Similarly, `inc` is used as the action because we want to
increment the counter by one rather than by using the value of `time`.

```yaml
- regex:
    expression: "^.*(?P<order_success>order successful).*$"
- metrics:
    succesful_orders_total:
      type: Counter
      description: "log lines with the message `order successful`"
      source: order_success
      config:
        action: inc
```

This pipeline first tries to find `order successful` in the log line, extracting
it as the `order_success` field in the extracted map. The metrics stage then
creates a metric called `succesful_orders_total` whose value only increases when
`order_success` was found in the extracted map.

The result of this pipeline is a metric whose value only increases when a log
line with the text `order successful` was scraped by Promtail.

```yaml
- regex:
    expression: "^.* order_status=(?P<order_status>.*?) .*$"
- metrics:
    succesful_orders_total:
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
        fail: fail
        action: inc
```

This pipeline first tries to find text in the format `order_status=<value>` in
the log line, pulling out the `<value>` into the extracted map with the key
`order_status`.

The metric stages creates `succesful_orders_total` and `failed_orders_total`
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
