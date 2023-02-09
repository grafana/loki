---
title: sampling
description: sampling stage
---
# sampling

The `sampling` stage is a stage that sampling logs. 

## Sampling stage schema

```yaml
sampling:
  # The rate sampling in lines per second that Promtail will push to Loki.
  # The value is between 0 and 1.
  [rate: <int>]
```

## Examples

The following are examples showing the use of the `sampling` stage.

### sampling

Simple `sampling` stage configurations.

#### Match a line and throttle

Given the pipeline:

```yaml
- sampling:
    rate: 0.1
```
