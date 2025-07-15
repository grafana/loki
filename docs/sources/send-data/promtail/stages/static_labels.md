---
title: static_labels
menuTitle:  
description: The 'static_labels' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/static_labels/
weight:  
---

# static_labels

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The static_labels stage is an action stage that adds static-labels to the label set that is sent to Loki with the log entry.

## Schema

```yaml
static_labels:
  [ <string>: [<string>] ... ]
```

### Examples

For the given pipeline:

```yaml
- json:
    expressions:
      stream: stream
- labels:
    stream:
- static_labels:
    custom_key: custom_val
```

Given the following log line:

```
{"log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The first stage would extract `stream` into the extracted map with a value of
`stderr`. The `labels` stage would turn that key-value pair into a label. The
`static_labels` stage would add the provided static labels into the label set.

The resulting entry that is sent to Loki will contain `stream="stderr"` and `custom_key="custom_val"` as labels.
