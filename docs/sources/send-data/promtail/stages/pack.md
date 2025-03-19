---
title: pack
menuTitle:  
description: The 'pack' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/pack/
weight:  
---

# pack

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `pack` stage is a transform stage which lets you embed extracted values and labels into the log line by packing the log line and labels inside a JSON object.

For example, if you wanted to remove the labels `container` and `pod` but still wanted to keep their values you could use this stage to create the following output:

```json
{
  "container": "myapp",
  "pod": "pod-3223f",
  "_entry": "original log message"
}
```

The original message will be stored under the `_entry` key.

This stage is useful if you have some label or other metadata you would like to keep but it doesn't make a good label (isn't useful for querying or is too high cardinality)

The querying capabilities of Loki make it easy to still access this data and filter/aggregate on it at query time.

## Pack stage schema

```yaml
pack:
  # Name from extracted data and/or line labels
  # Labels provided here are automatically removed from the output labels.
  labels:
    - [<string>]

  # If the resulting log line should use any existing timestamp or use time.Now() when the line was processed.
  # To avoid out-of-order issues with Loki, when combining several log streams (separate source files) into one
  # you will want to set a new timestamp on the log line, `ingest_timestamp: true`
  # If you are not combining multiple source files or you know your log lines won't have interlaced timestamps
  # you can set this value to false.
  [ingest_timestamp: <bool> | default = true]
```

## Examples

Removing the container label and embed it into the log line (Kubernetes pods could have multiple containers)

```yaml
pack:
  labels:
    - container
```

This would create a log line

```json
{
  "container": "myapp",
  "_entry": "original log message"
}
```

**Loki 2.2 also includes a new [`unpack` parser](../../../../query/log_queries/#unpack) to work with the pack stage.**

For example:

```logql
{cluster="us-central1", job="myjob"} | unpack
```

Will automatically unpack embedded labels and log line and replace the log line with the original log line automatically.

### More Examples

Use the packed labels for filtering:

```logql
{cluster="us-central1", job="myjob"} | unpack | container="myapp"
```

You can even use the `json` parser twice if your original message was json:

```logql
{cluster="us-central1", job="myjob"} | unpack | container="myapp" | json | val_from_original_log_json="foo"
```

Or any other parser

```logql
{cluster="us-central1", job="myjob"} | unpack | container="myapp" | logfmt | val_from_original_log_json="foo"
```
