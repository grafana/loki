---
title: structured_metadata
description: The 'structured_metadata' Promtail pipeline stage
---

# structured_metadata

{{% admonition type="warning" %}}
Structured metadata is an experimental feature and is subject to change in future releases of Grafana Loki.
{{% /admonition %}}

The `structured_metadata` stage is an action stage that takes data from the extracted map and
modifies the [structured metadata]({{< relref "../../../get-started/labels/structured-metadata" >}}) that is sent to Loki with the log entry.

{{% admonition type="warning" %}}
Structured metadata will be rejected by Loki unless you enable the `allow_structured_metadata` per tenant configuration (in the `limits_config`).
{{% /admonition %}}

## Schema

```yaml
structured_metadata:
  # Key is REQUIRED and the name for the label of structured metadata that will be created.
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
      traceID: traceID
- labels:
    stream:
- structured_metadata:
    traceID:
```

Given the following log line:

```
{"log":"log message\n","stream":"stderr","traceID":"0242ac120002",time":"2019-04-30T02:12:41.8443515Z"}
```

The first stage would extract `stream` with a value of `stderr` and `traceID` with a value of `0242ac120002` into
the extracted data set. The `labels` stage would turn that `stream` and `stderr` key-value pair into a stream label.
The `structured_metadata` stage would attach the `traceID` and `0242ac120002` key-value pair as a structured metadata to the log line.
