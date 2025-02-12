---
title: labels
menuTitle:  
description: The 'labels' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/labels/
weight:  
---

# labels

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The labels stage is an action stage that takes data from the extracted map and
modifies the label set that is sent to Loki with the log entry.

## Schema

```yaml
labels:
  # Key is REQUIRED and the name for the label that will be created.
  # Value is optional and will be the name from extracted data whose value
  # will be used for the value of the label. If empty, the value will be
  # inferred to be the same as the key.
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
```

Given the following log line:

```
{"log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The first stage would extract `stream` into the extracted map with a value of
`stderr`. The labels stage would turn that key-value pair into a label, so the
log line sent to Loki would include the label `stream` with a value of `stderr`.
