---
title: match
menuTitle:  
description: The 'match' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/match/
weight:  
---

# match

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The match stage is a filtering stage that conditionally applies a set of stages
or drop entries when a log entry matches a configurable LogQL
[stream selector](../../../../query/log_queries/#log-stream-selector) and
[filter expressions](../../../../query/log_queries/#line-filter-expression).

{{< admonition type="note" >}}
The filters do not include label filter expressions such as `| label == "foobar"`.
{{< /admonition >}}

## Schema

```yaml
match:
  # LogQL stream selector and line filter expressions.
  selector: <string>

  # Names the pipeline. When defined, creates an additional label in
  # the pipeline_duration_seconds histogram, where the value is
  # concatenated with job_name using an underscore.
  [pipeline_name: <string>]

  # Determines what action is taken when the selector matches the log
  # line. Defaults to keep. When set to drop, entries will be dropped
  # and no later metrics will be recorded.
  # Stages must be not defined when dropping entries.
  [action: <string> | default = "keep"]
  
  # If you specify `action: drop` the metric `logentry_dropped_lines_total` 
  # will be incremented for every line dropped.  By default the reason
  # label will be `match_stage` however you can optionally specify a custom value 
  # to be used in the `reason` label of that metric here.
  [drop_counter_reason: <string> | default = "match_stage"]

  # Nested set of pipeline stages only if the selector
  # matches the labels of the log entries:
  stages:
    [<stages>...]
```

Refer to the [Promtail Stages Configuration Reference](./#promtail-pipeline-stages) for the
schema on the various stages supported here.

### Example

For the given pipeline:

```yaml
pipeline_stages:
- json:
    expressions:
      app:
- labels:
    app:
- match:
    selector: '{app="loki"}'
    stages:
    - json:
        expressions:
          msg: message
- match:
    pipeline_name: "app2"
    selector: '{app="pokey"}'
    action: keep
    stages:
    - json:
        expressions:
          msg: msg
- match:
    selector: '{app="promtail"} |~ ".*noisy error.*"'
    action: drop
    drop_counter_reason: promtail_noisy_error
- output:
    source: msg
```

And given log lines:

```json
{ "time":"2012-11-01T22:08:41+00:00", "app":"loki", "component": ["parser","type"], "level" : "WARN", "message" : "app1 log line" }
{ "time":"2012-11-01T22:08:41+00:00", "app":"promtail", "component": ["parser","type"], "level" : "ERROR", "message" : "foo noisy error" }
```

The first stage will add `app` with a value of `loki` into the extracted map for the first log line,
while the second stage will add `app` as a label (again with the value of `loki`).
The second line will follow the same flow and will be added the label `app` with a value of `promtail`.

The third stage uses LogQL to only execute the nested stages when there is a
label of `app` whose value is `loki`. This matches the first line in our case; the nested
`json` stage then adds `msg` into the extracted map with a value of `app1 log
line`.

The fourth stage uses LogQL to only executed the nested stages when there is a
label of `app` whose value is `pokey`. This does **not** match in our case, so
the nested `json` stage is not ran.

The fifth stage will drop any entries from the application `promtail` that matches
the regex `.*noisy error`. and will also increment the `logentry_dropped_lines_total` 
metric with a label `reason="promtail_noisy_error"`

The final `output` stage changes the contents of the log line to be the value of
`msg` from the extracted map. In this case, the log line is changed to `app1 log
line`.
