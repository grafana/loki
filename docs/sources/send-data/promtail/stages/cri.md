---
title: cri
menuTitle:  
description: The 'cri' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/cri/
weight:  
---

# cri

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `cri` stage is a parsing stage that reads the log line using the standard CRI logging format.

## Schema

```yaml
cri:
  # Max buffer size to hold partial lines.
  [max_partial_lines: <int> | default = 100]

  # Max line size to hold a single partial line, if max_partial_line_size_truncate is true. Example: 262144.
  [max_partial_line_size: <int> | default = 0]

  # Allows to pretruncate partial lines before storing in partial buffer.
  [max_partial_line_size_truncate: <bool> | default = false]
```

Unlike most stages, the `cri` stage provides no configuration options and only
supports the specific CRI log format. CRI specifies log lines as
space-delimited values with the following components:

1. `time`: The timestamp string of the log
1. `stream`: Either stdout or stderr
1. `flags`: CRI flags including F or P
1. `log`: The contents of the log line

No whitespace is permitted between the components. In the following example,
only the first log line can be properly formatted using the `cri` stage:

```
"2019-01-01T01:00:00.000000001Z stderr P test\ngood"
"2019-01-01 T01:00:00.000000001Z stderr testgood"
"2019-01-01T01:00:00.000000001Z testgood"
```

## Examples

For the given pipeline:

```yaml
- cri: {}
```

Given the following log line:

```
"2019-04-30T02:12:41.8443515Z stdout F message"
```

The following key-value pairs would be created in the set of extracted data:

- `content`: `message`
- `stream`: `stdout`
- `flags`: `F`
- `timestamp`: `2019-04-30T02:12:41.8443515` - The cri-stage both extracts the timestamp as a label and set it as the timestamp of the log entry.
