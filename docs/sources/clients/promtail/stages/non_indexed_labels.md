---
title: non_indexed_labels
description: non_indexed_labels stage
---
# non_indexed_labels

The labels stage is an action stage that takes data from the extracted map and
modifies the [non-indexed labels]({{< relref "../../../get-started/labels/non-indexed-labels" >}}) set that is sent to Loki with the log entry.

## Schema

```yaml
non_indexed_labels:
  # Key is REQUIRED and the name for the non-indexed label that will be created.
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
- non_indexed_labels:
    traceID:
```

Given the following log line:

```
{"log":"log message\n","stream":"stderr","traceID":"0242ac120002",time":"2019-04-30T02:12:41.8443515Z"}
```

The first stage would extract `stream` with a value of `stderr` and `traceID` with a value of `0242ac120002` into
the extracted data set. The `labels` stage would turn that `stream` and `stderr` key-value pair into a stream label.
The `non_indexed_labels` stage would attach the `traceID` and `0242ac120002` key-value pair as a non-indexed label metadata to the log line.
