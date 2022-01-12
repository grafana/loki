---
title: limit
---
# `limit` stage

The `limit` stage is a ratelimit stage that lets you throttle logs based on several options. 

## Limit stage schema

```yaml
limit:
  # Name from extracted data to parse. If empty, uses the log message.
  [source: <string>]
  
  # RE2 regular expression, if source is provided the regex will attempt to match the source
  # If no source is provided, then the regex attempts to match the log line
  # If the provided regex matches the log line or a provided source, the line will be throttled. 
  [expression: <string>]

  # value can only be specified when source is specified. It is an error to specify value and regex.
  # If the value provided is an exact match for the `source` the line will be throttled.
  [value: <string>]

  # allowed ingestion rate limit in lines per second
  [rate: <int>]

  # allowed ingestion burst lines
  [burst: <int>]

  # When drop is equal to true, logs that exceed the current ratelimit will be discarded.
  # When drop is equal to false, the log that exceeds the current ratelimit will only wait to enter the backpressure mode 
  [drop: <bool> | default = false]
```
