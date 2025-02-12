---
title: sampling
menuTitle:  
description: The 'sampling' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/sampling/
weight:  
---

# sampling

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `sampling` stage is a stage that sampling logs. 

## Sampling stage schema

The `sampling` stage is used to sampling the logs.  Configuring the value `rate: 0.1` means that 10% of the logs will be pushed to the Loki server.

```yaml
sampling:
  # The rate sampling in lines per second that Promtail will push to Loki.The value is between 0 and 1, where a value of 0 means no logs are sampled and a value of 1 means 100% of logs are sampled.
  [rate: <int>]  
```

## Examples

The following are examples showing the use of the `sampling` stage.

### sampling

#### Simple sampling

Given the pipeline:

```yaml
- sampling:
    rate: 0.1
```

#### Match a line and sampling

Given the pipeline:

```yaml
pipeline_stages:
- json:
    expressions:
      app:
- match:
    pipeline_name: "app2"
    selector: '{app="poki"}'
    stages:
    - sampling:
        rate: 0.1
```
Complex `sampling` stage configurations.

