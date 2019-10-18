# Pipelines

A detailed look at how to setup Promtail to process your log lines, including
extracting metrics and labels.

## Pipeline

A pipeline is used to transform a single log line, its labels, and its
timestamp. A pipeline is comprised of a set of **stages**. There are 4 types of
stages:

1. **Parsing stages** parse the current log line and extract data out of it. The
   extracted data is then available for use by other stages.
2. **Transform stages** transform extracted data from previous stages.
3. **Action stages** take extracted data from previous stages and do something
   with them. Actions can:
    1. Add or modify existing labels to the log line
    2. Change the timestamp of the log line
    3. Change the content of the log line
    4. Create a metric based on the extracted data
4. **Filtering stages** optionally apply a subset of stages or drop entries based on some
   condition.

Typical pipelines will start with a parsing stage (such as a
[regex](./stages/regex.md) or [json](./stages/json.md) stage) to extract data
from the log line. Then, a series of action stages will be present to do
something with that extracted data. The most common action stage will be a
[labels](./stages/labels.md) stage to turn extracted data into a label.

A common stage will also be the [match](./stages/match.md) stage to selectively
apply stages or drop entries based on a [LogQL stream selector and filter expressions](../../logql.md).

Note that pipelines can not currently be used to deduplicate logs; Loki will
receive the same log line multiple times if, for example:

1. Two scrape configs read from the same file
2. Duplicate log lines in a file are sent through a pipeline. Deduplication is
   not done.

However, Loki will perform some deduplication at query time for logs that have
the exact same nanosecond timestamp, labels, and log contents.

This documented example gives a good glimpse of what you can achieve with a
pipeline:

```yaml
scrape_configs:
- job_name: kubernetes-pods-name
  kubernetes_sd_configs: ....
  pipeline_stages:

  # This stage is only going to run if the scraped target has a label
  # of "name" with value "promtail".
  - match:
      selector: '{name="promtail"}'
      stages:
      # The regex stage parses out a level, timestamp, and component. At the end
      # of the stage, the values for level, timestamp, and component are only
      # set internally for the pipeline. Future stages can use these values and
      # decide what to do with them.
      - regex:
          expression: '.*level=(?P<level>[a-zA-Z]+).*ts=(?P<timestamp>[T\d-:.Z]*).*component=(?P<component>[a-zA-Z]+)'

      # The labels stage takes the level and component entries from the previous
      # regex stage and promotes them to a label. For example, level=error may
      # be a label added by this stage.
      - labels:
          level:
          component:

      # Finally, the timestamp stage takes the timestamp extracted from the
      # regex stage and promotes it to be the new timestamp of the log entry,
      # parsing it as an RFC3339Nano-formatted value.
      - timestamp:
          format: RFC3339Nano
          source: timestamp

  # This stage is only going to run if the scraped target has a label of
  # "name" with a value of "nginx" and if the log line contains the word "GET"
  - match:
      selector: '{name="nginx"} |= "GET"'
      stages:
      # This regex stage extracts a new output by matching against some
      # values and capturing the rest.
      - regex:
          expression: \w{1,3}.\w{1,3}.\w{1,3}.\w{1,3}(?P<output>.*)

      # The output stage changes the content of the captured log line by
      # setting it to the value of output from the regex stage.
      - output:
          source: output

  # This stage is only going to run if the scraped target has a label of
  # "name" with a value of "jaeger-agent".
  - match:
      selector: '{name="jaeger-agent"}'
      stages:
      # The JSON stage reads the log line as a JSON string and extracts
      # the "level" field from the object for use in further stages.
      - json:
          expressions:
            level: level

      # The labels stage pulls the value from "level" that was extracted
      # from the previous stage and promotes it to a label.
      - labels:
          level:
- job_name: kubernetes-pods-app
  kubernetes_sd_configs: ....
  pipeline_stages:
  # This stage will only run if the scraped target has a label of "app"
  # with a name of *either* grafana or prometheus.
  - match:
      selector: '{app=~"grafana|prometheus"}'
      stages:
      # The regex stage will extract a level and component for use in further
      # stages, allowing the level to be defined as either lvl=<level> or
      # level=<level> and the component to be defined as either
      # logger=<component> or component=<component>
      - regex:
          expression: ".*(lvl|level)=(?P<level>[a-zA-Z]+).*(logger|component)=(?P<component>[a-zA-Z]+)"

      # The labels stage then promotes the level and component extracted from
      # the regex stage to labels.
      - labels:
          level:
          component:

  # This stage will only run if the scraped target has a label "app"
  # with a value of "some-app" and the log line doesn't contains the word "info"
  - match:
      selector: '{app="some-app"} != "info"'
      stages:
      # The regex stage tries to extract a Go panic by looking for panic:
      # in the log message.
      - regex:
          expression: ".*(?P<panic>panic: .*)"

      # The metrics stage is going to increment a panic_total metric counter
      # which Promtail exposes. The counter is only incremented when panic
      # was extracted from the regex stage.
      - metrics:
        - panic_total:
            type: Counter
            description: "total count of panic"
            source: panic
            config:
              action: inc
```

### Data Accessible to Stages

The following sections further describe the types that are accessible to each
stage (although not all may be used):

#### Label Set

The current set of labels for the log line. Initialized to be the set of labels
that were scraped along with the log line. The label set is only modified by an
action stage, but filtering stages read from it.

The final label set will be index by Loki and can be used for queries.

#### Extracted Map

A collection of key-value pairs extracted during a parsing stage. Subsequent
stages operate on the extracted map, either transforming them or taking action
with them. At the end of a pipeline, the extracted map is discarded; for a
parsing stage to be useful, it must always be paired with at least one action
stage.

The extracted map is initialized with the same set of initial labels that were
scraped along with the log line. This initial data allows for taking action on
the values of labels inside pipeline stages that only manipulate the extracted
map. For example, log entries tailed from files have the label `filename` whose
value is the file path that was tailed. When a pipeline executes for that log
entry, the initial extracted map would contain `filename` using the same value
as the label.

#### Log Timestamp

The current timestamp for the log line. Action stages can modify this value.
If left unset, it defaults to the time when the log was scraped.

The final value for the timestamp is sent to Loki.

#### Log Line

The current log line, represented as text. Initialized to be the text that
Promtail scraped. Action stages can modify this value.

The final value for the log line is sent to Loki as the text content for the
given log entry.

## Stages

Parsing stages:

  * [docker](./stages/docker.md): Extract data by parsing the log line using the standard Docker format.
  * [cri](./stages/cri.md): Extract data by parsing the log line using the standard CRI format.
  * [regex](./stages/regex.md): Extract data using a regular expression.
  * [json](./stages/json.md): Extract data by parsing the log line as JSON.

Transform stages:

  * [template](./stages/template.md): Use Go templates to modify extracted data.

Action stages:

  * [timestamp](./stages/timestamp.md): Set the timestamp value for the log entry.
  * [output](./stages/output.md): Set the log line text.
  * [labels](./stages/labels.md): Update the label set for the log entry.
  * [metrics](./stages/metrics.md): Calculate metrics based on extracted data.

Filtering stages:

  * [match](./stages/match.md): Conditionally run stages based on the label set.
