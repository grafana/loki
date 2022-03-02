---
title: limit
---
# `limit` stage

The `limit` stage is a rate limiting stage throttles logs based on several options. 

## Limit stage schema

```yaml
limit:
  # The allowed ingestion rate limit in lines per second
  [rate: <int>]

  # The quantity of allowed ingestion burst lines
  [burst: <int>]

  # When drop is true, log lines that exceed the current rate limit will be discarded.
  # When drop is false, log lines that exceed the current rate limit will only wait
  # to enter the back pressure mode. 
  [drop: <bool> | default = false]
```

## Examples

The following are examples showing the use of the `limit` stage.

### limit

Simple `limit` stage configurations.

#### Match a line and throttle

Given the pipeline:

```yaml
- limit:
    rate: 10
    burst: 10
```

Would throttle any log line.

#### Regex match a line and drop

Given the pipeline:

```yaml
- limit:
    rate: 10
    burst: 10
    drop: true
```

Would throttle any log line and drop logs when rate limit.
