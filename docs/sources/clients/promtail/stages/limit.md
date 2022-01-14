---
title: limit
---
# `limit` stage

The `limit` stage is a rate limiting stage throttles logs based on several options. 

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

## Examples

The following are examples showing the use of the `limit` stage.

### Simple limit

Simple `limit` stage configurations only specify one of the options, or two options when using the `source` option.

#### Regex match a line and throttle

Given the pipeline:

```yaml
- limit:
    expression: ".*debug.*"
    rate: 10
    burst: 10
```

Would throttle any log line with the word `debug` in it.

#### Regex match a line and drop

Given the pipeline:

```yaml
- limit:
    expression: ".*debug.*"
    rate: 10
    burst: 10
    drop: true
```

Would throttle any log line with the word `debug` in it and drop logs when rate limit.

#### Regex match a source

Given the pipeline:

```yaml
- json:
    expressions:
     level:
     msg:
- drop:
    source:     "level"
    expression: "(error|ERROR)"
    rate: 10
    burst: 10
```

Would throttle both of these log lines:

```
{"time":"2019-01-01T01:00:00.000000001Z", "level": "error", "msg":"11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"}
{"time":"2019-01-01T01:00:00.000000001Z", "level": "ERROR", "msg":"11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"}
```

#### Value match a source

Given the pipeline:

```yaml
- json:
    expressions:
     level:
     msg:
- drop:
    source: "level"
    value:  "error"
    rate: 10
    burst: 10
```

Would throttle this log line:

```
{"time":"2019-01-01T01:00:00.000000001Z", "level": "error", "msg":"11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"}
```
