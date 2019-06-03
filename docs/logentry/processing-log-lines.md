# Processing Log Lines

  * [Pipeline](#pipeline)
  * [Stages](#stages)

## Pipeline

Pipeline stages implement the following interface:

```go
type Stage interface {
	Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)
}
```

Any Stage is capable of modifying the `labels`, `extracted` data, `time`, and/or `entry`, though generally a Stage should only modify one of those things to reduce complexity.

Typical pipelines will start with a [regex](#regex) or [json](#json) stage to extract data from the log line.  Then any combination of other stages follow to use the data in the `extracted` map.  It may also be common to see the use of [match](#match) at the start of a pipeline to selectively apply stages based on labels. 

The example below gives a good glimpse of what you can achieve with a pipeline :

```yaml
scrape_configs:
- job_name: kubernetes-pods-name
  kubernetes_sd_configs: ....
  pipeline_stages:
  - match:
      selector: '{name="promtail"}'
      stages:
      - regex:
          expression: ".*level=(?P<level>[a-zA-Z]+).*ts=(?P<timestamp>[T\d-:.Z]*).*component=(?P<component>[a-zA-Z]+)"
      - labels:
          level:
          component:
      - timestamp:
          format: RFC3339Nano
          source: timestamp
  - match:
      selector: '{name="nginx"}'
      stages:
      - regex:
          expression: \w{1,3}.\w{1,3}.\w{1,3}.\w{1,3}(?P<output>.*)
      - output:
          source: output
  - match:
      selector: '{name="jaeger-agent"}'
      stages:
      - json:
          expressions:
            level: level
      - labels:
          level:
- job_name: kubernetes-pods-app
  kubernetes_sd_configs: ....
  pipeline_stages:
  - match:
      selector: '{app=~"grafana|prometheus"}'
      stages:
      - regex:
          expression: ".*(lvl|level)=(?P<level>[a-zA-Z]+).*(logger|component)=(?P<component>[a-zA-Z]+)"
      - labels:
          level:
          component:
  - match:
      selector: '{app="some-app"}'
      stages:
      - regex:
          expression: ".*(?P<panic>panic: .*)"
      - metrics:
        - panic_total:
            type: Counter
            description: "total count of panic"
            source: panic
            config:
              action: inc
```

In the first job:

The first `match` stage will only run if a label named `name` == `promtail`, it then applies a regex to parse the line, followed by setting two labels (level and component) and the timestamp from extracted data.

The second `match` stage will only run if a label named `name` == `nginx`, it then parses the log line with regex and extracts the `output` which is then set as the log line output sent to loki

The third `match` stage will only run if label named `name` == `jaeger-agent`, it then parses this log as JSON extracting `level` which is then set as a label

In the second job:

The first `match` stage will only run if a label named `app` == `grafana` or `prometheus`, it then parses the log line with regex, and sets two new labels of level and component from the extracted data.

The second `match` stage will only run if a label named `app` == `some-app`, it then parses the log line and creates an extracted key named panic if it finds `panic: ` in the log line.  Then a metrics stage will increment a counter if the extracted key `panic` is found in the `extracted` map.

More info on each field in the interface:

##### labels

A set of prometheus style labels which will be sent with the log line and will be indexed by Loki.

##### extracted

metadata extracted during the pipeline execution which can be used by subsequent stages.  This data is not sent with the logs and is dropped after the log entry is processed through the pipeline.

For example, stages like [regex](#regex) and [json](#json) will use expressions to extract data from a log line and store it in the `extracted` map, which following stages like [timestamp](#timestamp) or [output](#output) can use to manipulate the log lines `time` and `entry`.

##### time

The timestamp which loki will store for the log line, if not set within the pipeline using the [time](#time) stage, it will default to time.Now().

##### entry

The log line which will be stored by loki, the [output](#output) stage is capable of modifying this value, if no stage modifies this value the log line stored will match what was input to the system and not be modified.

## Stages

Extracting data (for use by other stages)

  * [regex](#regex) - use regex to extract data
  * [json](#json) - parse a JSON log and extract data

Filtering stages

  * [match](#match) - apply selectors to conditionally run stages based on labels

Mutating/manipulating output
  
  * [timestamp](#timestamp) - set the timestamp sent to Loki
  * [output](#output) - set the log content sent to Loki

Adding Labels

  * [labels](#labels) - add labels to the log stream

Metrics

  * [metrics](#metrics) - calculate metrics from the log content

### regex

A regex stage will take the provided regex and set the named groups as data in the `extracted` map.

```yaml
- regex:
    expression:  ①
```

① `expression` is **required** and needs to be a [golang RE2 regex string](https://github.com/google/re2/wiki/Syntax). Every capture group `(re)` will be set into the `extracted` map, every capture group **must be named:** `(?P<name>re)`, the name will be used as the key in the map.

##### Example:

```yaml
- regex:
    expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$"
```
  
Log line: `2019-01-01T01:00:00.000000001Z stderr P i'm a log message!`

Would create the following `extracted` map:

```go
{
	"time":    "2019-01-01T01:00:00.000000001Z",
	"stream":  "stderr",
	"flags":   "P",
	"content": "i'm a log message",
}
```

These map entries can then be used by other pipeline stages such as [timestamp](#timestamp) and/or [output](#output) 

[Example in unit test](../../pkg/logentry/stages/regex_test.go)

### json

A json stage will take the provided [JMESPath expressions](http://jmespath.org/) and set the key/value data in the `extracted` map.

```yaml
- json:
    expressions:        ①
      key: expression   ②
```

① `expressions` is a required yaml object containing key/value pairs of JMESPath expressions  
② `key: expression` where `key` will be the key in the `extracted` map, and the value will be the evaluated JMESPath expression.

This stage uses the Go JSON unmarshaller, which means non string types like numbers or booleans will be unmarshalled into those types.  The `extracted` map will accept non-string values and this stage will keep primitive types as they are unmarshalled (e.g. bool or float64).  Downstream stages will need to perform correct type conversion of these values as necessary.

If the value is a complex type, for example a JSON object, it will be marshalled back to JSON before being put in the `extracted` map.

##### Example:

```yaml
- json:
    expressions:
      output: log
      stream: stream
      timestamp: time
```

Log line: `{"log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}`

Would create the following `extracted` map:

```go
{
	"output":    "log message\n",
	"stream":    "stderr",
	"timestamp": "2019-04-30T02:12:41.8443515"
}
```
[Example in unit test](../../pkg/logentry/stages/json_test.go)

### match

A match stage will take the provided label `selector` and determine if a group of provided Stages will be executed or not based on labels

```yaml
- match:
    selector: "{app=\"loki\"}"       ①
    pipeline_name: loki_pipeline     ②
    stages:                          ③
```
① `selector` is **required** and must be a [logql stream selector](../usage.md#log-stream-selector).  
② `pipeline_name` is **optional** but when defined, will create an additional label on the `pipeline_duration_seconds` histogram, the value for `pipeline_name` will be concatenated with the `job_name` using an underscore: `job_name`_`pipeline_name`   
③ `stages` is a **required** list of additional pipeline stages which will only be executed if the defined `selector` matches the labels.  The format is a list of pipeline stages which is defined exactly the same as the root pipeline


[Example in unit test](../../pkg/logentry/stages/match_test.go)


### timestamp

A timestamp stage will parse data from the `extracted` map and set the `time` value which will be stored by Loki.

```yaml
- timestamp:
    source:   ①
    format:   ②
```

① `source` is **required** and is the key name to data in the `extracted` map.  
② `format` is **required** and is the input to Go's [time.parse](https://golang.org/pkg/time/#Parse) function.

Several of Go's pre-defined format's can be used by their name:

```go
ANSIC       = "Mon Jan _2 15:04:05 2006"
UnixDate    = "Mon Jan _2 15:04:05 MST 2006"
RubyDate    = "Mon Jan 02 15:04:05 -0700 2006"
RFC822      = "02 Jan 06 15:04 MST"
RFC822Z     = "02 Jan 06 15:04 -0700" // RFC822 with numeric zone
RFC850      = "Monday, 02-Jan-06 15:04:05 MST"
RFC1123     = "Mon, 02 Jan 2006 15:04:05 MST"
RFC1123Z    = "Mon, 02 Jan 2006 15:04:05 -0700" // RFC1123 with numeric zone
RFC3339     = "2006-01-02T15:04:05Z07:00"
RFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
```

##### Example:

```yaml
- timestamp:
    source: time
    format: RFC3339Nano
```

This stage would be placed after the [regex](#regex) example stage above, and the resulting `extracted` map _time_ value would be stored by Loki.


[Example in unit test](../../pkg/logentry/stages/timestamp_test.go)

### output

An output stage will take data from the `extracted` map and set the `entry` value which be stored by Loki.

```yaml
- output:
    source:    ①
```

① `source` is **required** and is the key name to data in the `extracted` map.

##### Example:

```yaml
- output:
    source: content
```

This stage would be placed after the [regex](#regex) example stage above, and the resulting `extracted` map _content_ value would be stored as the log value by Loki.

[Example in unit test](../../pkg/logentry/stages/output_test.go)

### labels

A label stage will take data from the `extracted` map and set additional `labels` on the log line.

```yaml
- labels:
    label_name: source   ①②
```

① `label_name` is **required** and will be the name of the label added.  
② `"source"` is **optional**, if not provided the label_name is used as the source key into the `extracted` map

##### Example:

```yaml
- labels:
    stream:
```

This stage when placed after the [regex](#regex) example stage above, would create the following `labels`:

```go
{
	"stream": "stderr",
}
```

[Example in unit test](../../pkg/logentry/stages/labels_test.go)

### metrics

A metrics stage will define and update metrics from `extracted` data.

[Simple example in unit test](../../pkg/logentry/stages/metrics_test.go)

Several metric types are available:

#### Counter

```yaml
- metrics:
    counter_name:    ①
      type: Counter  ②
      description:   ③
      source:        ④
      config:
        value:       ⑤
        action:      ⑥
```

① `counter_name` is **required** and should be set to the desired counters name.  
② `type` is **required** and should be the word `Counter` (case insensitive).  
③ `description` is **optional** but recommended.  
④ `source` is **optional** and is will be used as the key in the `extracted` data map, if not provided it will default to the `counter_name`.  
⑤ `value` is **optional**, if present, the metric will only be operated on if `value` == `extracted[source]`.  For example, if `value` is _panic_ then the counter will only be modified if `extracted[source] == "panic"`.  
⑥ `action` is **required** and must be either `inc` or `add` (case insensitive).  If `add` is chosen, the value of the `extracted` data will be used as the parameter to the method and therefore must be convertible to a positive float.

##### Examples

```yaml
- metrics:
    log_lines_total:   
      type: Counter
      description: "total number of log lines"
      source: time 
      config:
        action: inc
```

This counter will increment whenever the _time_ key is present in the `extracted` map, since every log entry should have a timestamp this is a good field to pick if you wanted to count every line.  Notice `value` is missing here because we don't care what the value is, we want to match every timestamp.  Also we use `inc` because we are not interested in the value of the extracted _time_ field.

```yaml
- regex:
    expression: "^.*(?P<order_success>order successful).*$"
- metrics:
    succesful_orders_total:   
      type: Counter
      description: "log lines with the message `order successful`"
      source: order_success 
      config:
        action: inc
```

This combo regex and counter would count any log line which has the words `order successful` in it.

```yaml
- regex:
    expression: "^.* order_status=(?P<order_status>.*?) .*$"
- metrics:
    succesful_orders_total:   
      type: Counter
      description: "successful orders"
      source: order_status 
      config:
        value: success
        action: inc
    failed_orders_total:   
      type: Counter
      description: "failed orders"
      source: order_status 
      config:
        fail: fail
        action: inc
```

Similarly, this would look for a key=value pair of `order_status=success` or `order_status=fail` and increment each counter respectively.

#### Gauge

```yaml
- metrics:
    gauge_name:      ①
      type: Gauge    ②
      description:   ③
      source:        ④
      config:
        value:       ⑤
        action:      ⑥
```

① `gauge_name` is **required** and should be set to the desired counters name.  
② `type` is **required** and should be the word `Gauge` (case insensitive).  
③ `description` is **optional** but recommended.  
④ `source` is **optional** and is will be used as the key in the `extracted` data map, if not provided it will default to the `gauge_name`.  
⑤ `value` is **optional**, if present, the metric will only be operated on if `value` == `extracted[source]`.  For example, if `value` is _panic_ then the counter will only be modified if `extracted[source] == "panic"`.  
⑥ `action` is **required** and must be either `set`, `inc`, `dec`, `add` or `sub` (case insensitive).  If `add`, `set`, or `sub`, is chosen, the value of the `extracted` data will be used as the parameter to the method and therefore must be convertible to a positive float.

##### Example

Gauge examples will be very similar to Counter examples with additional `action` values

#### Histogram

```yaml
- metrics:
    histogram_name:    ①
      type: Histogram  ②
      description:     ③
      source:          ④
      config:
        value:         ⑤
        buckets: []    ⑥⑦
```

① `histogram_name` is **required** and should be set to the desired counters name.  
② `type` is **required** and should be the word `Histogram` (case insensitive).  
③ `description` is **optional** but recommended.  
④ `source` is **optional** and is will be used as the key in the `extracted` data map, if not provided it will default to the `histogram_name`.  
⑤ `value` is **optional**, if present, the metric will only be operated on if `value` == `extracted[source]`.  For example, if `value` is _panic_ then the counter will only be modified if `extracted[source] == "panic"`.  
⑥ `action` is **required** and must be either `inc` or `add` (case insensitive).  If `add` is chosen, the value of the `extracted` data will be used as the parameter in `add()` and therefore must be convertible to a numeric type.  
⑦ bucket values should be an array of numeric type

##### Example

```yaml
- metrics:
    http_response_time_seconds:
      type: Histogram
      description: "length of each log line"
      source: response_time
      config:
        buckets: [0.001,0.0025,0.005,0.010,0.025,0.050]
```

This would create a Histogram which looks for _response_time_ in the `extracted` data and applies the value to the histogram.