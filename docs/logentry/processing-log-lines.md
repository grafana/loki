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

Stages are grouped into a pipeline which will execute a group of stages.

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

  * [match](#match)
  * [regex](#regex)
  * [json](#json)
  * [timestamp](#timestamp)
  * [output](#output)
  * [labels](#labels)
  * [metrics](#metrics)  

### match

A match stage will take the provided label `selector` and determine if a group of provided Stages will be executed or not based on labels

```yaml
- match:
    selector: "{app=\"loki\"}"       ①
    pipeline_name: "loki_pipeline"   ②
    stages:                          ③
```
① `selector` is **required** and uses logql label matcher expressions TODO LINK  
② `piplne_name` is **optional** but when defined, will create an additional label on the `pipeline_duration_seconds` histogram, the value for `pipeline_name` will be concatenated with the `job_name` using an underscore: `job_name`_`pipeline_name`   
③ `stages` is a **required** list of additional pipeline stages which will only be executed if the defined `selector` matches the labels.  The format is a list of pipeline stages which is defined exactly the same as the root pipeline


[Example in unit test](../../pkg/logentry/match_test.go)

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

TODO need example in unit test

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
      output: "log"
      stream: "stream"
      timestamp: "time"
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
TODO need example in unit test

### timestamp

A timestamp stage will parse data from the `extracted` map and set the `time` value.

### output

An output stage will take data from the `extracted` map and set the `entry` value, which is what will be stored by Loki.

### labels

A label stage will take data from the `extracted` map and set additional `labels` on the log line.

### metrics

A metrics stage will define and update metrics from `extracted` data.