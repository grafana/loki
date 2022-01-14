---
title: limit
---
# `limit` stage

The `limit` stage is a ratelimit stage that lets you throttle logs based on several options. 

## Limit stage schema

```yaml
limit:
  # Name from the extracted data to parse. If empty, it parses the log message.
  [source: <string>]
  
  # An RE2 regular expression. If the source is provided, the regex will attempt to match
  # the source. If no source is provided, then the regex attempts to match the log line.
  # If the provided regex matches the log line or a provided source, the line will
  # be throttled. 
  [expression: <string>]

  # A value can only be specified when `source` is specified. It is an error to specify
  # both `value` and `expression`. If the value provided is an exact match for
  # the `source`, the line will be throttled.
  [value: <string>]

  # The allowed ingestion rate limit in lines per second
  [rate: <int>]

  # The quantity of allowed ingestion burst lines
  [burst: <int>]

  # When drop is true, log lines that exceed the current rate limit will be discarded.
  # When drop is false, log lines that exceed the current rate limit will only wait
  # to enter the back pressure mode. 
  [drop: <bool> | default = false]
```
