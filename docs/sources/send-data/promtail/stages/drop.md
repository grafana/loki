---
title: drop
menuTitle:  
description: The 'drop' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/drop/
weight: 
---

# drop

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `drop` stage is a filtering stage that lets you drop logs based on several options.

It's important to note that if you provide multiple options they will be treated like an AND clause,
where each option has to be true to drop the log.

If you wish to drop with an OR clause, then specify multiple drop stages.

There are examples below to help explain.

## Drop stage schema

```yaml
drop:
  # Single name or names list of extracted data. If empty, uses the log message.
  [source: [<string>] | <string>]

  # Separator placed between concatenated extracted data names. The default separator is a semicolon.
  [separator: <string> | default = ";"]

  # RE2 regular expression. If `source` is provided and it's a list, the regex will attempt to match
  # the concatenated sources. If no source is provided, then the regex attempts
  # to match the log line.
  # If the provided regex matches the log line or the source, the line will be dropped.
  [expression: <string>]

  # value can only be specified when source is specified. If `source` is provided and it's a list,
  # the value will attempt to match the concatenated sources. It is an error to specify value and expression.
  # If the value provided is an exact match for the `source` the line will be dropped.
  [value: <string>]

  # older_than will be parsed as a Go duration: https://golang.org/pkg/time/#ParseDuration
  # If the log line timestamp is older than the current time minus the provided duration it will be dropped.
  [older_than: <duration>]

  # longer_than is a value in bytes, any log line longer than this value will be dropped.
  # Can be specified as an exact number of bytes in integer format: 8192
  # Or can be expressed with a suffix such as 8kb
  [longer_than: <string>|<int>]

  # Every time a log line is dropped the metric `logentry_dropped_lines_total`
  # will be incremented.  By default the reason label will be `drop_stage`
  # however you can optionally specify a custom value to be used in the `reason`
  # label of that metric here.
  [drop_counter_reason: <string> | default = "drop_stage"]
```

## Examples

The following are examples showing the use of the `drop` stage.

### Simple drops

Simple `drop` stage configurations only specify one of the options, or two options when using the `source` option.

Given the pipeline:

```yaml
- drop:
    source: ["level","msg"]
```

Drops any log line that has an extracted data field of at least `level` or `msg`.

#### Regex match a line

This example pipeline drops any log line with the substring "debug" in it:

```yaml
- drop:
    expression: ".*debug.*"
```


#### Regex match concatenated sources

Given the pipeline:

```yaml
- json:
    expressions:
     level:
     msg:
- drop:
    source:     ["level","msg"]
    separator:   "#"
    expression:  "(error|ERROR)#.*\/loki\/api\/push.*"
```

Drops both of these log lines:

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
```

Would drop this log line:

```
{"time":"2019-01-01T01:00:00.000000001Z", "level": "error", "msg":"11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"}
```

#### Drop old log lines

{{< admonition type="note" >}}
For `older_than` to work, you must be using the [timestamp](../timestamp/) stage to set the timestamp from the ingested log line _before_ applying the `drop` stage.
{{< /admonition >}}

Given the pipeline:

```yaml
- json:
    expressions:
     time:
     msg:
- timestamp:
    source: time
    format: RFC3339
- drop:
    older_than: 24h
    drop_counter_reason: "line_too_old"
```

With a current ingestion time of 2020-08-12T12:00:00Z would drop this log line when read from a file:

```
{"time":"2020-08-11T11:00:00Z", "level": "error", "msg":"11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"}
```

However it would _not_ drop this log line:

```
{"time":"2020-08-11T13:00:00Z", "level": "error", "msg":"11.11.11.11 - "POST /loki/api/push/ HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"}
```

In this example the current time is 2020-08-12T12:00:00Z and `older_than` is 24h. All log lines which have a timestamp older than 2020-08-11T12:00:00Z will be dropped.

All lines dropped by this drop stage would also increment the `logentry_dropped_lines_total` metric with a label `reason="line_too_old"`

#### Dropping long log lines

Given the pipeline:

```yaml
- drop:
    longer_than: 8kb
    drop_counter_reason: "line_too_long"
```

Would drop any log line longer than 8kb bytes, this is useful when Loki would reject a line for being too long.

All lines dropped by this drop stage would also increment the `logentry_dropped_lines_total` metric with a label `reason="line_too_long"`

### Complex drops

Complex `drop` stage configurations specify multiple options in one stage or specify multiple drop stages

#### Drop logs by regex AND length

Given the pipeline:

```yaml
- drop:
    expression: ".*debug.*"
    longer_than: 1kb
```

Would drop all logs that contain the word _debug_ *AND* are longer than 1kb bytes

#### Drop logs by time OR length OR regex

Given the pipeline:

```yaml
- json:
    expressions:
     time:
     msg:
- timestamp:
    source: time
    format: RFC3339
- drop:
    older_than: 24h
- drop:
    longer_than: 8kb
- drop:
    source: "msg"
    expression: ".*trace.*"
```

Would drop all logs older than 24h OR longer than 8kb bytes OR have a json `msg` field containing the word _trace_
