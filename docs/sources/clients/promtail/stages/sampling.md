---
title: sampling
description: sampling stage
---
# sampling

The `sampling` stage is a stage that sampling logs. 

## Sampling stage schema

The `sampling` stage is used to sampling the logs. Currently, only one rate param. rate: 0.1 means that 10% of the logs can be pushed to the loki server.

```yaml
sampling:
  # The rate sampling in lines per second that Promtail will push to Loki.The value is between 0 and 1.
  [rate: <int>]
  # The rate sampling in lines per second that Promtail will push to Loki.The value is between 0 and 1.
  [rate: <int>]
  # The rate sampling in lines per second that Promtail will push to Loki.The value is between 0 and 1.
  [rate: <int>]
  # The rate sampling in lines per second that Promtail will push to Loki.The value is between 0 and 1.
  [rate: <int>]
```
```yaml
drop:
  # The rate sampling in lines per second that Promtail will push to Loki.The value is between 0 and 1.
  [rate: <int>]

  # value can only be specified when source is specified. If `source` is provided and it's a list,
  # the value will attempt to match the concatenated sources. It is an error to specify value and expression.
  # If the value provided is an exact match for the `source` the line will be dropped.
  [value: <string>]

  # older_than will be parsed as a Go duration: https://golang.org/pkg/time/#ParseDuration
  # If the log line timestamp is older than the current time minus the provided duration it will be dropped.
  [older_than: <duration>]

  # longer_than is a value in bytes, any log line longer than this value will be dropped.
  # Can be specified as an exact number of bytes in integer format: 8192
  # Or can be expressed with a suffix such as 8kb
  [longer_than: <string>|<int>]

  # Every time a log line is dropped the metric `logentry_dropped_lines_total`
  # will be incremented.  By default the reason label will be `drop_stage`
  # however you can optionally specify a custom value to be used in the `reason`
  # label of that metric here.
  [drop_counter_reason: <string> | default = "drop_stage"]
```


## Examples

The following are examples showing the use of the `sampling` stage.

### sampling

Simple `sampling` stage configurations.

#### Match a line and sampling

Given the pipeline:

```yaml
- sampling:
    rate: 0.1
```
