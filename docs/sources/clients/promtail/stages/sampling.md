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
