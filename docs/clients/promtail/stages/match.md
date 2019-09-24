# `match` stage

The match stage is a filtering stage that conditionally applies a set of stages
when a log entry matches a configurable [LogQL](../../../logql.md) stream
selector.

## Schema

```yaml
match:
  # LogQL stream selector.
  selector: <string>

  # Names the pipeline. When defined, creates an additional label in
  # the pipeline_duration_seconds histogram, where the value is
  # concatenated with job_name using an underscore.
  [pipieline_name: <string>]

  # Nested set of pipeline stages only if the selector
  # matches the labels of the log entries:
  stages:
    - [
        <regex_stage>
        <json_stage> |
        <template_stage> |
        <match_stage> |
        <timestamp_stage> |
        <output_stage> |
        <labels_stage> |
        <metrics_stage>
      ]
```

Refer to the [Promtail Configuration Reference](../configuration.md) for the
schema on the various other stages referenced here.

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
    selector: "{app=\"loki\"}"
    stages:
    - json:
        expressions:
          msg: message
- match:
    pipeline_name: "app2"
    selector: "{app=\"pokey\"}"
    stages:
    - json:
        expressions:
          msg: msg
- output:
    source: msg
```

And the given log line:

```
{ "time":"2012-11-01T22:08:41+00:00", "app":"loki", "component": ["parser","type"], "level" : "WARN", "message" : "app1 log line" }
```

The first stage will add `app` with a value of `loki` into the extracted map,
while the second stage will add `app` as a label (again with the value of `loki`).

The third stage uses LogQL to only execute the nested stages when there is a
label of `app` whose value is `loki`. This matches in our case; the nested
`json` stage then adds `msg` into the extracted map with a value of `app1 log
line`.

The fourth stage uses LogQL to only executed the nested stages when there is a
label of `app` whose value is `pokey`. This does **not** match in our case, so
the nested `json` stage is not ran.

The final `output` stage changes the contents of the log line to be the value of
`msg` from the extracted map. In this case, the log line is changed to `app1 log
line`.
