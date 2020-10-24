---
title: LogQL
weight: 700
---
# LogQL: Log Query Language

Loki comes with its own PromQL-inspired language for queries called *LogQL*.
LogQL can be considered a distributed `grep` that aggregates log sources.
LogQL uses labels and operators for filtering.

There are two types of LogQL queries:

- *Log queries* return the contents of log lines.
- *Metric queries* extend log queries and calculate sample values based on the content of logs from a log query.

## Log Queries

A basic log query consists of two parts:

- **log stream selector**
- **log pipeline**

Due to Loki's design, all LogQL queries must contain a **log stream selector**.

The log stream selector determines how many log streams (unique sources of log content, such as files) will be searched.
A more granular log stream selector then reduces the number of searched streams to a manageable volume.
This means that the labels passed to the log stream selector will affect the relative performance of the query's execution.

**Optionally** the log stream selector can be followed by a **log pipeline**. A log pipeline is a set of stage expressions chained together and applied to the selected log streams. Each expressions can filter out, parse and mutate log lines and their respective labels.

The following example shows a full log query in action:

```logql
{container="query-frontend",namespace="tempo-dev"} |= "metrics.go" | logfmt | duration > 10s and throughput_mb < 500
```

The query is composed of:

- a log stream selector `{container="query-frontend",namespace="loki-dev"}` which targets the `query-frontend` container  in the `loki-dev`namespace.
- a log pipeline `|= "metrics.go" | logfmt | duration > 10s and throughput_mb < 500` which will filter out log that doesn't contains the word `metrics.go`, then parses each log line to extract more labels and filter with them.

> To avoid escaping special characters you can use the `` ` ``(back-tick) instead of `"` when quoting strings.
For example `` `\w+` `` is the same as `"\\w+"`.
This is specially useful when writing a regular expression which contains multiple backslashes that require escaping.

### Log Stream Selector

The log stream selector determines which log streams should be included in your query results.
The stream selector is comprised of one or more key-value pairs, where each key is a **log label** and each value is that **label's value**.

The log stream selector is written by wrapping the key-value pairs in a pair of curly braces:

```logql
{app="mysql",name="mysql-backup"}
```

In this example, all log streams that have a label of `app` whose value is `mysql` _and_ a label of `name` whose value is `mysql-backup` will be included in the query results.
Note that this will match any log stream whose labels _at least_ contain `mysql-backup` for their name label;
if there are multiple streams that contain that label, logs from all of the matching streams will be shown in the results.

The `=` operator after the label name is a **label matching operator**.
The following label matching operators are supported:

- `=`: exactly equal.
- `!=`: not equal.
- `=~`: regex matches.
- `!~`: regex does not match.

Examples:

- `{name=~"mysql.+"}`
- `{name!~"mysql.+"}`
- `` {name!~`mysql-\d+`} ``

The same rules that apply for [Prometheus Label Selectors](https://prometheus.io/docs/prometheus/latest/querying/basics/#instant-vector-selectors) apply for Loki log stream selectors.

### Log Pipeline

A log pipeline can be appended to a log stream selector to further process and filter log streams. It usually is composed of one or multiple expressions, each expressions is executed in sequence for each log line. If an expression filters out a log line, the pipeline will stop at this point and start processing the next line.

Some expressions can mutate the log content and respective labels (e.g `| line_format "{{.status_code}}"`), which will be then available for further filtering and processing following expressions or metric queries.

A log pipeline can be composed of:

- [Line Filter Expression](#Line-Filter-Expression).
- [Parser Expression](#Parser-Expression)
- [Label Filter Expression](#Label-Filter-Expression)
- [Line Format Expression](#Line-Format-Expression)
- [Labels Format Expression](#Labels-Format-Expression)
- [Unwrap Expression](#Unwrap-Expression)

The [unwrap Expression](#Unwrap-Expression) is a special expression that should only be used within metric queries.

#### Line Filter Expression

The line filter expression is used to do a distributed `grep` over the aggregated logs from the matching log streams.

After writing the log stream selector, the resulting set of logs can be further filtered with a search expression.
The search expression can be just text or regex:

- `{job="mysql"} |= "error"`
- `{name="kafka"} |~ "tsdb-ops.*io:2003"`
- `` {name="cassandra"} |~  `error=\w+` ``
- `{instance=~"kafka-[23]",name="kafka"} != "kafka.server:type=ReplicaManager"`

In the previous examples, `|=`, `|~`, and `!=` act as **filter operators**.
The following filter operators are supported:

- `|=`: Log line contains string.
- `!=`: Log line does not contain string.
- `|~`: Log line matches regular expression.
- `!~`: Log line does not match regular expression.

Filter operators can be chained and will sequentially filter down the expression - resulting log lines must satisfy _every_ filter:

```logql
{job="mysql"} |= "error" != "timeout"
```

When using `|~` and `!~`, Go (as in [Golang](https://golang.org/)) [RE2 syntax](https://github.com/google/re2/wiki/Syntax) regex may be used.
The matching is case-sensitive by default and can be switched to case-insensitive prefixing the regex with `(?i)`.

While line filter expressions could be placed anywhere in a pipeline, it is almost always better to have them at the beginning. This ways it will improve the performance of the query doing further processing only when a line matches.

For example, while the result will be the same, the following query `{job="mysql"} |= "error" | json | line_format "{{.err}}"` will always run faster than  `{job="mysql"} | json | line_format "{{.message}}"` |= "error"`. Line filter expressions are the fastest way to filter logs after log stream selectors.

#### Parser Expression

Parser expression can parse and extract labels from the log content. Those extracted labels can then be used for filtering using [label filter expressions](#Label-Filter-Expression) or for [metric aggregations](#Metric-Queries).

Extracted label keys are automatically sanitized by all parsers, to follow Prometheus metric name convention.(They can only contain ASCII letters and digits, as well as underscores and colons. They cannot start with a digit.)

For instance, the pipeline `| json` will produce the following mapping:
```json
{ "a.b": {c: "d"}, e: "f" }
```
->
```
{a_b_c="d", e="f"}
```

In case of errors, for instance if the line is not in the expected format, the log line won't be filtered but instead will get a new `__error__` label added.

If an extracted label key name already exists in the original log stream, the extracted label key will be suffixed with the `_extracted` keyword to make the distinction between the two labels. You can forcefully override the original label using a [label formatter expression](#Labels-Format-Expression). However if an extracted key appears twice, only the latest label value will be kept.

We support currently support json, logfmt and regexp parsers.

The **json** parsers take no parameters and can be added using the expression `| json` in your pipeline. It will extract all json properties as labels if the log line is a valid json document. Nested properties are flattened into label keys using the `_` separator. **Arrays are skipped**.

For example the json parsers will extract from the following document:

```json
{
	"protocol": "HTTP/2.0",
	"servers": ["129.0.1.1","10.2.1.3"],
	"request": {
		"time": "6.032",
		"method": "GET",
		"host": "foo.grafana.net",
		"size": "55",
	},
	"response": {
		"status": 401,
		"size": "228",
		"latency_seconds": "6.031"
	}
}
```

The following list of labels:

```kv
"protocol" => "HTTP/2.0"
"request_time" => "6.032"
"request_method" => "GET"
"request_host" => "foo.grafana.net"
"request_size" => "55"
"response_status" => "401"
"response_size" => "228"
"response_size" => "228"
```

The **logfmt** parser can be added using the `| logfmt` and will extract all keys and values from the [logfmt](https://brandur.org/logfmt) formatted log line.

For example the following log line:

```logfmt
at=info method=GET path=/ host=grafana.net fwd="124.133.124.161" connect=4ms service=8ms status=200
```

will get those labels extracted:

```kv
"at" => "info"
"method" => "GET"
"path" => "/"
"host" => "grafana.net"
"fwd" => "124.133.124.161"
"service" => "8ms"
"status" => "200"
```

Unlike the logfmt and json, which extract implicitly all values and takes no parameters, the **regexp** parser takes a single parameter `| regexp "<re>"` which is the regular expression using the [Golang](https://golang.org/) [RE2 syntax](https://github.com/google/re2/wiki/Syntax).

The regular expression must contain a least one named sub-match (e.g `(?P<name>re)`), each sub-match will extract a different label.

For example the parser `| regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)"` will extract from the following line:

```log
POST /api/prom/api/v1/query_range (200) 1.5s
```

those labels:

```kv
"method" => "POST"
"path" => "/api/prom/api/v1/query_range"
"status" => "200"
"duration" => "1.5s"
```

It's easier to use the predefined parsers like `json` and `logfmt` when you can, falling back to `regexp` when the log lines have unusual structure. Multiple parsers can be used during the same log pipeline which is useful when you want to parse complex logs. ([see examples](#Multiple-parsers))

#### Label Filter Expression

Label filter expression allows filtering log line using their original and extracted labels. It can contains multiple predicates.

A predicate contains a **label identifier**, an **operation** and a **value** to compare the label with.

For example with `cluster="namespace"` the cluster is the label identifier, the operation is `=` and the value is "namespace". The label identifier is always on the right side of the operation.

We support multiple **value** types which are automatically inferred from the query input.

- **String** is double quoted or backticked such as `"200"` or \``us-central1`\`.
- **[Duration](https://golang.org/pkg/time/#ParseDuration)** is a sequence of decimal numbers, each with optional fraction and a unit suffix, such as "300ms", "1.5h" or "2h45m". Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
- **Number** are floating-point number (64bits), such as`250`, `89.923`.
- **Bytes** is a sequence of decimal numbers, each with optional fraction and a unit suffix, such as "42MB", "1.5Kib" or "20b". Valid bytes units are "b", "kib", "kb", "mib", "mb", "gib",  "gb", "tib", "tb", "pib", "pb", "eib", "eb".

String type work exactly like Prometheus label matchers use in [log stream selector](#Log-Stream-Selector). This means you can use the same operations (`=`,`!=`,`=~`,`!~`).

> The string type is the only one that can filter out a log line with a label `__error__`.

Using Duration, Number and Bytes will convert the label value prior to comparision and support the following comparators:

- `==` or `=` for equality.
- `!=` for inequality.
- `>` and `>=` for greater than and greater than or equal.
- `<` and `<=` for lesser than and lesser than or equal.

For instance, `logfmt | duration > 1m and bytes_consumed > 20MB`

If the conversion of the label value fails, the log line is not filtered and an `__error__` label is added. To filters those errors see the [pipeline errors](#Pipeline-Errors) section.

You can chain multiple predicates using `and` and `or` which respectively express the `and` and `or` binary operations. `and` can be equivalently expressed by a comma, a space or another pipe. Label filters can be place anywhere in a log pipeline.

This means that all the following expressions are equivalent:

```logql
| duration >= 20ms or size == 20kb and method!~"2.."
| duration >= 20ms or size == 20kb | method!~"2.."
| duration >= 20ms or size == 20kb , method!~"2.."
| duration >= 20ms or size == 20kb  method!~"2.."

```

By default the precedence of multiple predicates is right to left. You can wrap predicates with parenthesis to force a different precedence left to right.

For example the following are equivalent.

```logql
| duration >= 20ms or method="GET" and size <= 20KB
| ((duration >= 20ms or method="GET") and size <= 20KB)
```

It will evaluate first `duration >= 20ms or method="GET"`. To evaluate first `method="GET" and size <= 20KB`, make sure to use proper parenthesis as shown below.

```logql
| duration >= 20ms or (method="GET" and size <= 20KB)
```

> Label filter expressions are the only expression allowed after the [unwrap expression](#Unwrap-Expression). This is mainly to allow filtering errors from the metric extraction (see [errors](#Pipeline-Errors)).

#### Line Format Expression

The line format expression can rewrite the log line content by using the [text/template](https://golang.org/pkg/text/template/) format.
It takes a single string parameter `| line_format "{{.label_name}}"`, which is the template format. All labels are injected variables into the template and are available to use with the `{{.label_name}}` notation.

For example the following expression:

```logql
{container="frontend"} | logfmt | line_format "{{.query}} {{.duration}}"
```

Will extract and rewrite the log line to only contains the query and the duration of a request.

You can use double quoted string for the template or single backtick \``\{{.label_name}}`\` to avoid the need to escape special characters.

See [functions](#Template-functions) to learn about available functions in the template format.

#### Labels Format Expression

The `| label_format` expression can renamed, modify or add labels. It takes as parameter a comma separated list of equality operations, enabling multiple operations at once.

When both side are label identifiers, for example `dst=src`, the operation will rename the `src` label into `dst`.

The left side can alternatively be a template string (double quoted or backtick), for example `dst="{{.status}} {{.query}}"`, in which case the `dst` label value will be replace by the result of the [text/template](https://golang.org/pkg/text/template/) evaluation. This is the same template engine as the `| line_format` expression, this mean labels are available as variables and you can use the same list of [functions](#Template-functions).

In both case if the destination label doesn't exist a new one will be created.

The renaming form `dst=src` will _drop_ the `src` label after remapping it to the `dst` label. However, the _template_ form will preserve the referenced labels, such that  `dst="{{.src}}"` results in both `dst` and `src` having the same value.

> A single label name can only appear once per expression. This means `| label_format foo=bar,foo="new"` is not allowed but you can use two expressions for the desired effect: `| label_format foo=bar | label_format foo="new"`

#### Template functions

The text template format used in `| line_format` and `| label_format` support functions the following list of functions.

##### ToLower & ToUpper

Convert the entire string to lowercase or uppercase:

Examples:

```template
"{{.request_method | ToLower}}"
"{{.request_method | ToUpper}}"
`{{ToUpper "This is a string" | ToLower}}`
```

##### Replace

Perform simple string replacement.

It takes three arguments:

- string to replace
- string to replace with
- source string

Example:

```template
`"This is a string" | Replace " " "-"`
```

The above will produce `This-is-a-string`

##### Trim

`Trim` returns a slice of the string s with all leading and
trailing Unicode code points contained in cutset removed.

`TrimLeft` and `TrimRight` are the same as `Trim` except that it respectively trim only leading and trailing characters.

```template
`{{ Trim .query ",. " }}`
`{{ TrimLeft .uri ":" }}`
`{{ TrimRight .path "/" }}`
```

`TrimSpace` TrimSpace returns string s with all leading
and trailing white space removed, as defined by Unicode.

```template
{{ TrimSpace .latency }}
```

`TrimPrefix` and `TrimSuffix` will trim respectively the prefix or suffix supplied.

```template
{{ TrimPrefix .path "/" }}
```

##### Regex

`regexReplaceAll` returns a copy of the input string, replacing matches of the Regexp with the replacement string replacement. Inside string replacement, $ signs are interpreted as in Expand, so for instance $1 represents the text of the first sub-match. See the golang [docs](https://golang.org/pkg/regexp/#Regexp.ReplaceAll) for detailed examples.

```template
`{{ regexReplaceAllLiteral "(a*)bc" .some_label "${1}a" }}`
```

`regexReplaceAllLiteral` returns a copy of the input string, replacing matches of the Regexp with the replacement string replacement The replacement string is substituted directly, without using Expand.

```template
`{{ regexReplaceAllLiteral "(ts=)" .timestamp "timestamp=" }}`
```

You can combine multiple function using pipe, for example if you want to strip out spaces and make the request method in capital you would write the following template `{{ .request_method | TrimSpace | ToUpper }}`.

### Log Queries Examples

#### Multiple filtering

Filtering should be done first using label matchers, then line filters (when possible) and finally using label filters. The following query demonstrate this.

```logql
{cluster="ops-tools1", namespace="loki-dev", job="loki-dev/query-frontend"} |= "metrics.go" !="out of order" | logfmt | duration > 30s or status_code!="200"
```

#### Multiple parsers

To extract the method and the path of the following logfmt log line:

```log
level=debug ts=2020-10-02T10:10:42.092268913Z caller=logging.go:66 traceID=a9d4d8a928d8db1 msg="POST /api/prom/api/v1/query_range (200) 1.5s"
```

You can use multiple parsers (logfmt and regexp) like this.

```logql
{job="cortex-ops/query-frontend"} | logfmt | line_format "{{.msg}}" | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)"`
```

This is possible because the `| line_format` reformats the log line to become `POST /api/prom/api/v1/query_range (200) 1.5s` which can then be parsed with the `| regexp ...` parser.

#### Formatting

The following query shows how you can reformat a log line to make it easier to read on screen.

```logql
{cluster="ops-tools1", name="querier", namespace="loki-dev"}
  |= "metrics.go" != "loki-canary"
  | logfmt
  | query != ""
  | label_format query="{{ Replace .query \"\\n\" \"\" -1 }}"
  | line_format "{{ .ts}}\t{{.duration}}\ttraceID = {{.traceID}}\t{{ printf \"%-100.100s\" .query }} "
```

Label formatting is used to sanitize the query while the line format reduce the amount of information and creates a tabular output.

For those given log line:

```log
level=info ts=2020-10-23T20:32:18.094668233Z caller=metrics.go:81 org_id=29 traceID=1980d41501b57b68 latency=fast query="{cluster=\"ops-tools1\", job=\"cortex-ops/query-frontend\"} |= \"query_range\"" query_type=filter range_type=range length=15m0s step=7s duration=650.22401ms status=200 throughput_mb=1.529717 total_bytes_mb=0.994659
level=info ts=2020-10-23T20:32:18.068866235Z caller=metrics.go:81 org_id=29 traceID=1980d41501b57b68 latency=fast query="{cluster=\"ops-tools1\", job=\"cortex-ops/query-frontend\"} |= \"query_range\"" query_type=filter range_type=range length=15m0s step=7s duration=624.008132ms status=200 throughput_mb=0.693449 total_bytes_mb=0.432718
```

The result would be:

```log
2020-10-23T20:32:18.094668233Z	650.22401ms	    traceID = 1980d41501b57b68	{cluster="ops-tools1", job="cortex-ops/query-frontend"} |= "query_range"
2020-10-23T20:32:18.068866235Z	624.008132ms	traceID = 1980d41501b57b68	{cluster="ops-tools1", job="cortex-ops/query-frontend"} |= "query_range"
```

## Metric Queries

LogQL also supports wrapping a log query with functions that allow for creating metrics out of the logs.

Metric queries can be used to calculate things such as the rate of error messages, or the top N log sources with the most amount of logs over the last 3 hours.

Combined with log [parsers](#Parser-Expression), metrics queries can also be used to calculate metrics from a sample value within the log line such latency or request size.
Furthermore all labels, including extracted ones, will be available for aggregations and generation of new series.

### Range Vector aggregation

LogQL shares the same [range vector](https://prometheus.io/docs/prometheus/latest/querying/basics/#range-vector-selectors) concept from Prometheus, except the selected range of samples is a range of selected log or label values.

Loki supports two types of range aggregations. Log range and unwrapped range aggregations.

#### Log Range Aggregations

A log range is a log query (with or without a log pipeline) followed by the range notation e.g [1m]. It should be noted that the range notation `[5m]` can be placed at end of the log pipeline or right after the log stream matcher.

The first type uses log entries to compute values and supported functions for operating over are:

- `rate(log-range)`: calculates the number of entries per second
- `count_over_time(log-range)`: counts the entries for each log stream within the given range.
- `bytes_rate(log-range)`: calculates the number of bytes per second for each stream.
- `bytes_over_time(log-range)`: counts the amount of bytes used by each log stream for a given range.

##### Log  Examples

```logql
count_over_time({job="mysql"}[5m])
```

This example counts all the log lines within the last five minutes for the MySQL job.

```logql
sum by (host) (rate({job="mysql"} |= "error" != "timeout" | json | duration > 10s [1m]))
```

This example demonstrates a LogQL aggregation which includes filters and parsers.
It returns the per-second rate of all non-timeout errors within the last minutes per host for the MySQL job and only includes errors whose duration is above ten seconds.

#### Unwrapped Range Aggregations

Unwrapped ranges uses extracted labels as sample values instead of log lines. However to select which label will be use within the aggregation, the log query must end with an unwrap expression and optionally a label filter expression to discard [errors](#Pipeline-Errors).

The unwrap expression is noted `| unwrap label_identifier` where the label identifier is the label name to use for extracting sample values.

Since label values are string, by default a conversion into a float (64bits) will be attempted, in case of failure the `__error__` label is added to the sample.
Optionally the label identifier can be wrapped by a conversion function `| unwrap <function>(label_identifier)`, which will attempt to convert the label value from a specific format.

We currently support only the function `duration_seconds` (or its short equivalent `duration`) which will convert the label value in seconds from the [go duration format](https://golang.org/pkg/time/#ParseDuration) (e.g `5m`, `24s30ms`).

Supported function for operating over unwrapped ranges are:

- `sum_over_time(unwrapped-range)`: the sum of all values in the specified interval.
- `avg_over_time(unwrapped-range)`: the average value of all points in the specified interval.
- `max_over_time(unwrapped-range)`: the maximum value of all points in the specified interval.
- `min_over_time(unwrapped-range)`: the minimum value of all points in the specified interval
- `stdvar_over_time(unwrapped-range)`: the population standard variance of the values in the specified interval.
- `stddev_over_time(unwrapped-range)`: the population standard deviation of the values in the specified interval.
- `quantile_over_time(scalar,unwrapped-range)`: the φ-quantile (0 ≤ φ ≤ 1) of the values in the specified interval.

Except for `sum_over_time`, `min_over_time` and `max_over_time` unwrapped range aggregations support grouping.

```logql
<aggr-op>([parameter,] <unwrapped-range>) [without|by (<label list>)]
```

Which can be used to aggregate over distinct labels dimensions by including a `without` or `by` clause.

`without` removes the listed labels from the result vector, while all other labels are preserved the output. `by` does the opposite and drops labels that are not listed in the `by` clause, even if their label values are identical between all elements of the vector.

#### Unwrapped Examples

```logql
quantile_over_time(0.99,
  {cluster="ops-tools1",container="ingress-nginx"}
    | json
    | __error__ = ""
    | unwrap request_time [1m])) by (path)
```

This example calculates the p99 of the nginx-ingress latency by path.

```logql
sum by (org_id) (
  sum_over_time(
  {cluster="ops-tools1",container="loki-dev"}
      |= "metrics.go"
      | logfmt
      | unwrap bytes_processed [1m])
  )
```

This calculates the amount of bytes processed per organization id.

### Aggregation operators

Like [PromQL](https://prometheus.io/docs/prometheus/latest/querying/operators/#aggregation-operators), LogQL supports a subset of built-in aggregation operators that can be used to aggregate the element of a single vector, resulting in a new vector of fewer elements but with aggregated values:

- `sum`: Calculate sum over labels
- `min`: Select minimum over labels
- `max`: Select maximum over labels
- `avg`: Calculate the average over labels
- `stddev`: Calculate the population standard deviation over labels
- `stdvar`: Calculate the population standard variance over labels
- `count`: Count number of elements in the vector
- `bottomk`: Select smallest k elements by sample value
- `topk`: Select largest k elements by sample value

The aggregation operators can either be used to aggregate over all label values or a set of distinct label values by including a `without` or a `by` clause:

```logql
<aggr-op>([parameter,] <vector expression>) [without|by (<label list>)]
```

`parameter` is only required when using `topk` and `bottomk`.
`topk` and `bottomk` are different from other aggregators in that a subset of the input samples, including the original labels, are returned in the result vector.

`by` and `without` are only used to group the input vector.
The `without` clause removes the listed labels from the resulting vector, keeping all others.
The `by` clause does the opposite, dropping labels that are not listed in the clause, even if their label values are identical between all elements of the vector.

#### Vector Aggregations Examples

Get the top 10 applications by the highest log throughput:

```logql
topk(10,sum(rate({region="us-east1"}[5m])) by (name))
```

Get the count of logs for the last five minutes, grouping
by level:

```logql
sum(count_over_time({job="mysql"}[5m])) by (level)
```

Get the rate of HTTP GET of /home requests from NGINX logs by region:

```logql
avg(rate(({job="nginx"} |= "GET" | json | path="/home")[10s])) by (region)
```

### Binary Operators

#### Arithmetic Binary Operators

The following binary arithmetic operators exist in Loki:

- `+` (addition)
- `-` (subtraction)
- `*` (multiplication)
- `/` (division)
- `%` (modulo)
- `^` (power/exponentiation)

Binary arithmetic operators are defined between two literals (scalars), a literal and a vector, and two vectors.

Between two literals, the behavior is obvious:
They evaluate to another literal that is the result of the operator applied to both scalar operands (`1 + 1 = 2`).

Between a vector and a literal, the operator is applied to the value of every data sample in the vector, e.g. if a time series vector is multiplied by 2, the result is another vector in which every sample value of the original vector is multiplied by 2.

Between two vectors, a binary arithmetic operator is applied to each entry in the left-hand side vector and its matching element in the right-hand vector.
The result is propagated into the result vector with the grouping labels becoming the output label set. Entries for which no matching entry in the right-hand vector can be found are not part of the result.

Pay special attention to [operator order](#operator-order) when chaining arithmetic operators.

##### Arithmetic Examples

Implement a health check with a simple query:

```logql
1 + 1
```

Double the rate of a a log stream's entries:

```logql
sum(rate({app="foo"})) * 2
```

Get proportion of warning logs to error logs for the `foo` app

```logql
sum(rate({app="foo", level="warn"}[1m])) / sum(rate({app="foo", level="error"}[1m]))
```

#### Logical/set binary operators

These logical/set binary operators are only defined between two vectors:

- `and` (intersection)
- `or` (union)
- `unless` (complement)

`vector1 and vector2` results in a vector consisting of the elements of vector1 for which there are elements in vector2 with exactly matching label sets.
Other elements are dropped.

`vector1 or vector2` results in a vector that contains all original elements (label sets + values) of vector1 and additionally all elements of vector2 which do not have matching label sets in vector1.

`vector1 unless vector2` results in a vector consisting of the elements of vector1 for which there are no elements in vector2 with exactly matching label sets.
All matching elements in both vectors are dropped.

##### Binary Operators Examples

This contrived query will return the intersection of these queries, effectively `rate({app="bar"})`:

```logql
rate({app=~"foo|bar"}[1m]) and rate({app="bar"}[1m])
```

#### Comparison operators

- `==` (equality)
- `!=` (inequality)
- `>` (greater than)
- `>=` (greater than or equal to)
- `<` (less than)
- `<=` (less than or equal to)

Comparison operators are defined between scalar/scalar, vector/scalar, and vector/vector value pairs.
By default they filter.
Their behavior can be modified by providing `bool` after the operator, which will return 0 or 1 for the value rather than filtering.

Between two scalars, these operators result in another scalar that is either 0 (false) or 1 (true), depending on the comparison result.
The `bool` modifier must **not** be provided.

`1 >= 1` is equivalent to `1`

Between a vector and a scalar, these operators are applied to the value of every data sample in the vector, and vector elements between which the comparison result is false get dropped from the result vector.
If the `bool` modifier is provided, vector elements that would be dropped instead have the value 0 and vector elements that would be kept have the value 1.

Filters the streams which logged at least 10 lines in the last minute:

```logql
count_over_time({foo="bar"}[1m]) > 10
```

Attach the value(s) `0`/`1` to streams that logged less/more than 10 lines:

```logql
count_over_time({foo="bar"}[1m]) > bool 10
```

Between two vectors, these operators behave as a filter by default, applied to matching entries.
Vector elements for which the expression is not true or which do not find a match on the other side of the expression get dropped from the result, while the others are propagated into a result vector.
If the `bool` modifier is provided, vector elements that would have been dropped instead have the value 0 and vector elements that would be kept have the value 1, with the grouping labels again becoming the output label set.

Return the streams matching `app=foo` without app labels that have higher counts within the last minute than their counterparts matching `app=bar` without app labels:

```logql
sum without(app) (count_over_time({app="foo"}[1m])) > sum without(app) (count_over_time({app="bar"}[1m]))
```

Same as above, but vectors have their values set to `1` if they pass the comparison or `0` if they fail/would otherwise have been filtered out:

```logql
sum without(app) (count_over_time({app="foo"}[1m])) > bool sum without(app) (count_over_time({app="bar"}[1m]))
```

#### Operator order

When chaining or combining operators, you have to consider operator precedence:
Generally, you can assume regular [mathematical convention](https://en.wikipedia.org/wiki/Order_of_operations) with operators on the same precedence level being left-associative.

More details can be found in the [Golang language documentation](https://golang.org/ref/spec#Operator_precedence).

`1 + 2 / 3` is equal to `1 + ( 2 / 3 )`.

`2 * 3 % 2` is evaluated as `(2 * 3) % 2`.

### Pipeline Errors

There are multiple reasons which cause pipeline processing errors, such as:

- A numeric label filter may fail to turn a label value into a number
- A metric conversion for a label may fail.
- A log line is not a valid json document.
- etc...

When those failures happen, Loki won't filter out those log lines. Instead they are passed into the next stage of the pipeline with a new system label named `__error__`. The only way to filter out errors is by using a label filter expressions. The `__error__` label can't be renamed via the language.

For example to remove json errors:

```logql
  {cluster="ops-tools1",container="ingress-nginx"}
    | json
    | __error__ != "JSONParserErr"
```

Alternatively you can remove all error using a catch all matcher such as `__error__ = ""` or even show only errors using `__error__ != ""`.

The filter should be placed after the stage that generated this error. This means if you need to remove errors from an unwrap expression it needs to be placed after the unwrap.

```logql
quantile_over_time(
	0.99,
	{container="ingress-nginx",service="hosted-grafana"}
	| json
	| unwrap response_latency_seconds
	| __error__=""[1m]
	) by (cluster)
```

>Metric queries cannot contains errors, in case errors are found during execution, Loki will return an error and appropriate status code.
