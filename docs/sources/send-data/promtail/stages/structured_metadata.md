---
title: structured_metadata
description: The 'structured_metadata' Promtail pipeline stage
---

# structured_metadata

The `structured_metadata` stage is an action stage that takes data from the extracted map and
modifies the [structured metadata]({{< relref "../../../get-started/labels/structured-metadata" >}}) that is sent to Loki with the log entry.

{{< admonition type="warning" >}}
Structured metadata will be rejected by Loki unless you enable the `allow_structured_metadata` per tenant configuration (in the `limits_config`).

Structured metadata was added to chunk format V4 which is used if the schema version is greater or equal to **13**. (See Schema Config for more details about schema versions. )
{{< /admonition >}}

## Schema

```yaml
structured_metadata:
  # Key is REQUIRED and the name for the label of structured metadata that will be created.
  # Value is optional and will be the name from extracted data whose value
  # will be used for the value of the label. If empty, the value will be
  # inferred to be the same as the key.
  [ <string>: [<string>] ... ]
```

## Examples

### Parse from log entry

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

```json
{"log":"log message\n","stream":"stderr","traceID":"0242ac120002","time":"2019-04-30T02:12:41.8443515Z"}
```

The first stage would extract `stream` with a value of `stderr` and `traceID` with a value of `0242ac120002` into
the extracted data set. The `labels` stage would turn that `stream` and `stderr` key-value pair into a stream label.
The `structured_metadata` stage would attach the `traceID` and `0242ac120002` key-value pair as a structured metadata to the log line.

### Parse from service discovery labels

For the configuration below, you can use labels from `relabel_configs` to use as `structured_metadata`:

```yaml
pipeline_stages:
  - structured_metadata:
      pod_uid:
      pod_host_ip:
relabel_configs:
  - action: replace
    source_labels:
      - __meta_kubernetes_pod_uid
    target_label: pod_uid
  - action: replace
    source_labels:
      - __meta_kubernetes_pod_host_ip
    target_label: pod_host_ip
```

Given the following discovered labels below with a log line `sample log`:

|- Discovered label |- Value |- Target label |
| - | - | - |
| __meta_kubernetes_pod_host_ip | 127.0.0.1 | pod_host_ip |
| __meta_kubernetes_pod_uid | b3937321-fe90-4e15-ac94-495c8fdb9202 | pod_uid |

The `structured_metadata` stage would turn the discovered labels `pod_uid` and `pod_host_ip` into key-value pairs as  structured metadata for the log line `sample log` and
exclude them from creating high-cardinality streams.
