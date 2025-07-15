---
title: limit
menuTitle:  
description: The 'limit' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/limit/
weight:  
---

# limit

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `limit` stage is a rate-limiting stage that throttles logs based on several options. 

## Limit stage schema

This pipeline stage places limits on the rate or burst quantity of log lines that Promtail pushes to Loki.
The concept of having distinct burst and rate limits mirrors the approach to limits that can be set for the Loki distributor component:  `ingestion_rate_mb` and `ingestion_burst_size_mb`, as defined in [limits_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config).

```yaml
limit:
  # The rate limit in lines per second that Promtail will push to Loki
  [rate: <int>]

  # The cap in the quantity of burst lines that Promtail will push to Loki
  [burst: <int>]
   
  # Ratelimit each label value independently. If label is not found, log line is not
  # considered for ratelimiting. Drop must be true if this is set.
  [by_label_name: <string>]  
    
  # When ratelimiting by label is enabled, keep track of this many last used labels
  [max_distinct_labels: <int> | default = 10000]  

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

#### Match a line and drop

Given the pipeline:

```yaml
- limit:
    rate: 10
    burst: 10
    drop: true
```

Would throttle any log line and drop logs when rate limit.

#### Ratelimit by a label

Given the pipeline:

```yaml
- limit:
    rate: 10
    burst: 10
    drop: true
    by_label_name: "namespace"
```

Would ratelimit messages originating from each namespace independently.
Any message without namespace label will not be ratelimited.
