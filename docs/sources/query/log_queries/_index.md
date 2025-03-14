---
title: Log queries 
menuTItle:  
description: Overview of how log queries are constructed and parsed.
aliases: 
- ../logql/log_queries/
weight: 300 
---

# Log queries

All LogQL queries contain a **log stream selector**.

![parts of a query](./query_components.png)


Optionally, the log stream selector can be followed by a **log pipeline**. A log pipeline is a set of stage expressions that are chained together and applied to the selected log streams. Each expression can filter out, parse, or mutate log lines and their respective labels.

The following example shows a full log query in action:

```logql
{container="query-frontend",namespace="loki-dev"} |= "metrics.go" | logfmt | duration > 10s and throughput_mb < 500
```

The query is composed of:

- a log stream selector `{container="query-frontend",namespace="loki-dev"}` which targets the `query-frontend` container  in the `loki-dev` namespace.
- a log pipeline `|= "metrics.go" | logfmt | duration > 10s and throughput_mb < 500` which will filter out log that contains the word `metrics.go`, then parses each log line to extract more labels and filter with them.

> To avoid escaping special characters you can use the `` ` ``(backtick) instead of `"` when quoting strings.
For example `` `\w+` `` is the same as `"\\w+"`.
This is specially useful when writing a regular expression which contains multiple backslashes that require escaping.

## Log stream selector

The stream selector determines which log streams to include in a query's results.
A log stream is a unique source of log content, such as a file.
A more granular log stream selector then reduces the number of searched streams to a manageable volume.
This means that the labels passed to the log stream selector will affect the relative performance of the query's execution.

The log stream selector is specified by one or more comma-separated key-value pairs. Each key is a log label and each value is that label's value.
Curly braces (`{` and `}`) delimit the stream selector.

Consider this stream selector:

```logql
{app="mysql",name="mysql-backup"}
```

All log streams that have both a label of `app` whose value is `mysql`
and a label of `name` whose value is `mysql-backup` will be included in
the query results.
A stream may contain other pairs of labels and values,
but only the specified pairs within the stream selector are used to determine
which streams will be included within the query results.

The same rules that apply for [Prometheus Label Selectors](https://prometheus.io/docs/prometheus/latest/querying/basics/#instant-vector-selectors) apply for Grafana Loki log stream selectors.

The `=` operator after the label name is a **label matching operator**.
The following label matching operators are supported:

- `=`: exactly equal
- `!=`: not equal
- `=~`: regex matches
- `!~`: regex does not match

Regex log stream examples:

- `{name =~ "mysql.+"}`
- `{name !~ "mysql.+"}`
- `` {name !~ `mysql-\d+`} ``

**Note:** Unlike the [line filter regex expressions](#line-filter-expression), the `=~` and `!~` regex operators are fully anchored.
This means that the regex expression must match against the *entire* string, **including newlines**. 
The regex `.` character does not match newlines by default. If you want the regex dot character to match newlines you can use the single-line flag, like so: `(?s)search_term.+` matches `search_term\n`.
Alternatively, you can use the `\s` (match whitespaces, including newline) in combination with `\S` (match not whitespace characters) to match all characters, including newlines.
Refer to [Google's RE2 syntax](https://github.com/google/re2/wiki/Syntax) for more information.

Regex log stream newlines:

- `{name =~ ".*mysql.*"}`: does not match log label values with newline character
- `{name =~ "(?s).*mysql.*}`: match log label values with newline character
- `{name =~ "[\S\s]*mysql[\S\s]*}`: match log label values with newline character

## Log pipeline

A log pipeline can be appended to a log stream selector to further process and filter log streams. It is composed of a set of expressions. Each expression is executed in left to right sequence for each log line. If an expression filters out a log line, the pipeline will stop processing the current log line and start processing the next log line.

Some expressions can mutate the log content and respective labels,
which will be then be available for further filtering and processing in subsequent expressions.
An example that mutates is the expression

```
| line_format "{{.status_code}}"
```


Log pipeline expressions fall into one of four categories:

- Filtering expressions: [line filter expressions](#line-filter-expression)
and
[label filter expressions](#label-filter-expression)
- [Parsing expressions](#parser-expression)
- Formatting expressions: [line format expressions](#line-format-expression)
and
[label format expressions](#labels-format-expression)
- Labels expressions: [drop labels expression](#drop-labels-expression) and [keep labels expression](#keep-labels-expression)

### Line filter expression

The line filter expression does a distributed `grep`
over the aggregated logs from the matching log streams.
It searches the contents of the log line,
discarding those lines that do not match the case-sensitive expression.

Each line filter expression has a **filter operator**
followed by text or a regular expression.
These filter operators are supported:

- `|=`: Log line contains string
- `!=`: Log line does not contain string
- `|~`: Log line contains a match to the regular expression
- `!~`: Log line does not contain a match to the regular expression

**Note:** Unlike the [label matcher regex operators](#log-stream-selector), the `|~` and `!~` regex operators are not fully anchored.
This means that the `.` regex character matches all characters, **including newlines**.

Line filter expression examples:

- Keep log lines that have the substring "error":

    ```
    |= "error"
    ```

    A complete query using this example:

    ```
    {job="mysql"} |= "error"
    ```

- Discard log lines that have the substring "kafka.server:type=ReplicaManager":

    ```
    != "kafka.server:type=ReplicaManager"
    ```

    A complete query using this example:

    ```
    {instance=~"kafka-[23]",name="kafka"} != "kafka.server:type=ReplicaManager"
    ```

- Keep log lines that contain a substring that starts with `tsdb-ops` and ends with `io:2003`. A complete query with a regular expression:

    ```
    {name="kafka"} |~ "tsdb-ops.*io:2003"
    ```

- Keep log lines that contain a substring that starts with `error=`,
and is followed by 1 or more word characters. A complete query with a regular expression:

    ```
    {name="cassandra"} |~  `error=\w+`
    ```

Filter operators can be chained.
Filters are applied sequentially.
Query results will have satisfied every filter.
This complete query example will give results that include the string `error`,
and do not include the string `timeout`.

```logql
{job="mysql"} |= "error" != "timeout"
```

When using `|~` and `!~`, Go (as in [Golang](https://golang.org/)) [RE2 syntax](https://github.com/google/re2/wiki/Syntax) regex may be used.
The matching is case-sensitive by default.
Switch to case-insensitive matching by prefixing the regular expression
with `(?i)`.

While line filter expressions could be placed anywhere within a log pipeline,
it is almost always better to have them at the beginning.
Placing them at the beginning improves the performance of the query,
as it only does further processing when a line matches.
For example,
 while the results will be the same,
the query specified with

```
{job="mysql"} |= "error" | json | line_format "{{.err}}"
```

will always run faster than

```
{job="mysql"} | json | line_format "{{.message}}" |= "error"
```

Line filter expressions are the fastest way to filter logs once the
log stream selectors have been applied.

Line filter expressions have support matching IP addresses. See [Matching IP addresses](../ip/) for details.


### Removing color codes

Line filter expressions support stripping ANSI sequences (color codes) from
the line:

```
{job="example"} | decolorize
```

### Label filter expression

Label filter expression allows filtering log line using their original and extracted labels. It can contain multiple predicates.

A predicate contains a **label identifier**, an **operation** and a **value** to compare the label with.

For example with `cluster="namespace"` the cluster is the label identifier, the operation is `=` and the value is "namespace". The label identifier is always on the left side of the operation.

We support multiple **value** types which are automatically inferred from the query input.

- **String** is double quoted or backticked such as `"200"` or \``us-central1`\`.
- **[Duration](https://golang.org/pkg/time/#ParseDuration)** is a sequence of decimal numbers, each with optional fraction and a unit suffix, such as "300ms", "1.5h" or "2h45m". Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h". The value of the label identifier used for comparison must be a string with a unit suffix to be parsed correctly, such as "0.10ms" or "1h30m". Optionally, `label_format` can be used to modify the value and append the unit before making the comparison.
- **Number** are floating-point number (64bits), such as`250`, `89.923`.
- **Bytes** is a sequence of decimal numbers, each with optional fraction and a unit suffix, such as "42MB", "1.5KiB" or "20B". Valid bytes units are "B", "kB", "MB", "GB", "TB", "KB", "KiB", "MiB", "GiB", "TiB".

String type work exactly like Prometheus label matchers use in [log stream selector](#log-stream-selector). This means you can use the same operations (`=`,`!=`,`=~`,`!~`).

> The string type is the only one that can filter out a log line with a label `__error__`.

Using Duration, Number and Bytes will convert the label value prior to comparison and support the following comparators:

- `==` or `=` for equality.
- `!=` for inequality.
- `>` and `>=` for greater than and greater than or equal.
- `<` and `<=` for lesser than and lesser than or equal.

For instance, `logfmt | duration > 1m and bytes_consumed > 20MB`

If the conversion of the label value fails, the log line is not filtered and an `__error__` label is added. To filters those errors see the [pipeline errors](../#pipeline-errors) section.

You can chain multiple predicates using `and` and `or` which respectively express the `and` and `or` binary operations. `and` can be equivalently expressed by a comma, a space or another pipe. Label filters can be place anywhere in a log pipeline.

This means that all the following expressions are equivalent:

```logql
| duration >= 20ms or size == 20KB and method!~"2.."
| duration >= 20ms or size == 20KB | method!~"2.."
| duration >= 20ms or size == 20KB , method!~"2.."
| duration >= 20ms or size == 20KB  method!~"2.."

```

The precedence for evaluation of multiple predicates is left to right. You can wrap predicates with parenthesis to force a different precedence.

These examples are equivalent:

```logql
| duration >= 20ms or method="GET" and size <= 20KB
| ((duration >= 20ms or method="GET") and size <= 20KB)
```

To evaluate the logical `and` first, use parenthesis, as in this example:

```logql
| duration >= 20ms or (method="GET" and size <= 20KB)
```

> Label filter expressions are the only expression allowed after the unwrap expression. This is mainly to allow filtering errors from the metric extraction.

Label filter expressions have support matching IP addresses. See [Matching IP addresses](../ip/) for details.

### Parser expression

Parser expression can parse and extract labels from the log content. Those extracted labels can then be used for filtering using [label filter expressions](#label-filter-expression) or for [metric aggregations](../metric_queries/).

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

If an extracted label key name already exists in the original log stream, the extracted label key will be suffixed with the `_extracted` keyword to make the distinction between the two labels. You can forcefully override the original label using a [label formatter expression](#labels-format-expression). However, if an extracted key appears twice, only the first label value will be kept.

Loki supports  [JSON](#json), [logfmt](#logfmt), [pattern](#pattern), [regexp](#regular-expression) and [unpack](#unpack) parsers.

It's easier to use the predefined parsers `json` and `logfmt` when you can. If you can't, the `pattern` and `regexp` parsers can be used for log lines with an unusual structure. The `pattern` parser is easier and faster to write; it also outperforms the `regexp` parser.
Multiple parsers can be used by a single log pipeline. This is useful for parsing complex logs. There are examples in [Multiple parsers](../query_examples/#examples-that-use-multiple-parsers).

#### JSON

The **json** parser operates in two modes:

1. **without** parameters:

   Adding `| json` to your pipeline will extract all json properties as labels if the log line is a valid json document.
   Nested properties are flattened into label keys using the `_` separator.

   Note: **Arrays are skipped**.

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
           "headers": {
             "Accept": "*/*",
             "User-Agent": "curl/7.68.0"
           }
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
   "request_headers_Accept" => "*/*"
   "request_headers_User_Agent" => "curl/7.68.0"
   "response_status" => "401"
   "response_size" => "228"
   "response_latency_seconds" => "6.031"
   ```

2. **with** parameters:

   Using `| json label="expression", another="expression"` in your pipeline will extract only the
   specified json fields to labels. You can specify one or more expressions in this way, the same
   as [`label_format`](#labels-format-expression); all expressions must be quoted.

   Currently, we only support field access (`my.field`, `my["field"]`) and array access (`list[0]`), and any combination
   of these in any level of nesting (`my.list[0]["field"]`).

   For example, `| json first_server="servers[0]", ua="request.headers[\"User-Agent\"]` will extract from the following document:

    ```json
    {
        "protocol": "HTTP/2.0",
        "servers": ["129.0.1.1","10.2.1.3"],
        "request": {
            "time": "6.032",
            "method": "GET",
            "host": "foo.grafana.net",
            "size": "55",
            "headers": {
              "Accept": "*/*",
              "User-Agent": "curl/7.68.0"
            }
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
    "first_server" => "129.0.1.1"
    "ua" => "curl/7.68.0"
    ```

   If an array or an object returned by an expression, it will be assigned to the label in json format.

   For example, `| json server_list="servers", headers="request.headers"` will extract:

   ```kv
   "server_list" => `["129.0.1.1","10.2.1.3"]`
   "headers" => `{"Accept": "*/*", "User-Agent": "curl/7.68.0"}`
   ```
 
   If the label to be extracted is same as the original JSON field, expression can be written as just `| json <label>`

   For example, to extract `servers` fields as label, expression can be written as following
    
   `| json servers` will extract:

    ```kv
   "servers" => `["129.0.1.1","10.2.1.3"]`
   ```

   Note that `| json servers` is same as `| json servers="servers"`

#### logfmt

The **logfmt** parser can operate in two modes:

1. **without** parameters:

    The **logfmt** parser can be added using `| logfmt` and will extract all keys and values from the [logfmt](https://brandur.org/logfmt) formatted log line.

    For example the following log line:

    ```logfmt
    at=info method=GET path=/ host=grafana.net fwd="124.133.124.161" service=8ms status=200
    ```

    will result in having the following labels extracted:

    ```kv
    "at" => "info"
    "method" => "GET"
    "path" => "/"
    "host" => "grafana.net"
    "fwd" => "124.133.124.161"
    "service" => "8ms"
    "status" => "200"
    ```

2. **with** parameters:

    Similar to [JSON](#json), using `| logfmt label="expression", another="expression"` in the pipeline will result in extracting only the fields specified by the labels.

    For example, `| logfmt host, fwd_ip="fwd"` will extract the labels `host` and `fwd` from the following log line:
    ```logfmt
    at=info method=GET path=/ host=grafana.net fwd="124.133.124.161" service=8ms status=200
    ```

    And rename `fwd` to `fwd_ip`:
    ```kv
    "host" => "grafana.net"
    "fwd_ip" => "124.133.124.161"
    ```

The logfmt parser also supports the following flags:
- `--strict` to enable strict parsing

    With strict parsing enabled, the logfmt parser immediately stops scanning the log line and returns early with an error when it encounters any poorly formatted key/value pair.
    ```
    // accepted key/value pairs
    key=value key="value in double quotes"

    // invalid key/value pairs
    =value // no key
    foo=bar=buzz
    fo"o=bar
    ```

    Without the `--strict` flag the parser skips invalid key/value pairs and continues parsing the rest of the log line.
    Non-strict mode offers the flexibility to parse semi-structed log lines, though note that this is only best-effort.

- `--keep-empty` to retain standalone keys with empty value

    With `--keep-empty` flag set, the logfmt parser retains standalone keys(keys without a value) as labels with value set to empty string.
    If the standalone key is explicitly requested using label extraction parameters, there is no need to add this flag.

Note: flags if any should appear right after logfmt and before label extraction parameters
```
| logfmt --strict
| logfmt --strict host, fwd_ip="fwd"
| logfmt --keep-empty --strict host
```

#### Pattern

The pattern parser allows the explicit extraction of fields from log lines by defining a pattern expression (`| pattern "<pattern-expression>"`). The expression matches the structure of a log line.

Consider this NGINX log line.

```log
0.191.12.2 - - [10/Jun/2021:09:14:29 +0000] "GET /api/plugins/versioncheck HTTP/1.1" 200 2 "-" "Go-http-client/2.0" "13.76.247.102, 34.120.177.193" "TLSv1.2" "US" ""
```

This log line can be parsed with the expression

`<ip> - - <_> "<method> <uri> <_>" <status> <size> <_> "<agent>" <_>`

to extract these fields:

```kv
"ip" => "0.191.12.2"
"method" => "GET"
"uri" => "/api/plugins/versioncheck"
"status" => "200"
"size" => "2"
"agent" => "Go-http-client/2.0"
```

A pattern expression is composed of captures and literals.

A capture is a field name delimited by the `<` and `>` characters. `<example>` defines the field name `example`.
An unnamed capture appears as `<_>`. The unnamed capture skips matched content.

Captures are matched from the line beginning or the previous set of literals, to the line end or the next set of literals.
If a capture is not matched, the pattern parser will stop.

Literals can be any sequence of UTF-8 characters, including whitespace characters.

By default, a pattern expression is anchored at the start of the log line. If the expression starts with literals, then the log line must also start with the same set of literals. Use `<_>` at the beginning of the expression if you don't want to anchor the expression at the start.

Consider the log line

```log
level=debug ts=2021-06-10T09:24:13.472094048Z caller=logging.go:66 traceID=0568b66ad2d9294c msg="POST /loki/api/v1/push (204) 16.652862ms"
```

To match `msg="`, use the expression:

```pattern
<_> msg="<method> <path> (<status>) <latency>"
```

A pattern expression is invalid if

- It does not contain any named capture.
- It contains two consecutive captures not separated by whitespace characters.

#### Regular expression

Unlike the logfmt and json, which extract implicitly all values and takes no parameters, the regexp parser takes a single parameter `| regexp "<re>"` which is the regular expression using the [Golang](https://golang.org/) [RE2 syntax](https://github.com/google/re2/wiki/Syntax).

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

#### unpack

The `unpack` parser parses a JSON log line, unpacking all embedded labels from Promtail's [`pack` stage](../../send-data/promtail/stages/pack/).
**A special property `_entry` will also be used to replace the original log line**.

For example, using `| unpack` with the log line:

```json
{
  "container": "myapp",
  "pod": "pod-3223f",
  "_entry": "original log message"
}
```

extracts the `container` and `pod` labels; it sets `original log message` as the new log line.

You can combine the `unpack` and `json` parsers (or any other parsers) if the original embedded log line is of a specific format.

### Line format expression

The line format expression can rewrite the log line content by using the [text/template](https://golang.org/pkg/text/template/) format.
It takes a single string parameter `| line_format "{{.label_name}}"`, which is the template format. All labels are injected variables into the template and are available to use with the `{{.label_name}}` notation.

For example the following expression:

```logql
{container="frontend"} | logfmt | line_format "{{.query}} {{.duration}}"
```

Will extract and rewrite the log line to only contains the query and the duration of a request.

You can use double quoted string for the template or backticks `` `{{.label_name}}` `` to avoid the need to escape special characters.

`line_format` also supports `math` functions. Example:

If we have the following labels `ip=1.1.1.1`, `status=200` and `duration=3000`(ms), we can divide the duration by `1000` to get the value in seconds.

```logql
{container="frontend"} | logfmt | line_format "{{.ip}} {{.status}} {{div .duration 1000}}"
```

The above query will give us the `line` as `1.1.1.1 200 3`

Additionally, you can also access the log line using the [`__line__`](https://grafana.com/docs/loki/<LOKI_VERSION>/query/template_functions/#__line__) function and the timestamp using the [`__timestamp__`](https://grafana.com/docs/loki/<LOKI_VERSION>/query/template_functions/#__timestamp__) function. See [template functions](https://grafana.com/docs/loki/<LOKI_VERSION>/query/template_functions/) to learn about available functions in the template format.

### Labels format expression

The `| label_format` expression can rename, modify or add labels. It takes as parameter a comma separated list of equality operations, enabling multiple operations at once.

When both side are label identifiers, for example `dst=src`, the operation will rename the `src` label into `dst`.

The right side can alternatively be a template string (double quoted or backtick), for example `dst="{{.status}} {{.query}}"`, in which case the `dst` label value is replaced by the result of the [text/template](https://golang.org/pkg/text/template/) evaluation. This is the same template engine as the `| line_format` expression, which means labels are available as variables and you can use the same list of functions.

In both cases, if the destination label doesn't exist, then a new one is created.

The renaming form `dst=src` will _drop_ the `src` label after remapping it to the `dst` label. However, the _template_ form will preserve the referenced labels, such that  `dst="{{.src}}"` results in both `dst` and `src` having the same value.

> A single label name can only appear once per expression. This means `| label_format foo=bar,foo="new"` is not allowed but you can use two expressions for the desired effect: `| label_format foo=bar | label_format foo="new"`

### Drop Labels expression

**Syntax**:  `|drop name, other_name, some_name="some_value"`

The `| drop` expression will drop the given labels in the pipeline. For example, for the query `{job="varlogs"}|json|drop level, method="GET"`, with below log line

```
{"level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
```

the result will be

```
{host="grafana.net", path="/", status="200"} {"level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
```

Similarly, this expression can be used to drop `__error__` labels as well. For example, for the query `{job="varlogs"}|json|drop __error__`, with below log line

```
INFO GET / loki.net 200
```

the result will be

```
{} INFO GET / loki.net 200
```

Example with regex and multiple names

For the query `{job="varlogs"}|json|drop level, path, app=~"some-api.*"`, with below log lines

```
{"app": "some-api-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
{"app": "other-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
```

the result will be

```
{host="grafana.net", job="varlogs", method="GET", status="200"} {"app": "some-api-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
{app="other-service", host="grafana.net", job="varlogs", method="GET", status="200"} {"app": "other-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
```

### Keep Labels expression

**Syntax**:  `|keep name, other_name, some_name="some_value"`

The `| keep` expression will keep only the specified labels in the pipeline and drop all the other labels.

{{< admonition type="note" >}}
The keep stage will not drop the  __error__ or __error_details__ labels added by Loki at query time. To drop these labels, refer to [drop](#drop-labels-expression) stage.
{{< /admonition >}}

Query examples:

For the query `{job="varlogs"}|json|keep level, method="GET"`, with the following log lines:

```
{"level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
{"level": "info", "method": "POST", "path": "/", "host": "grafana.net", "status": "200"}
```

the result will be

```
{level="info", method="GET"} {"level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
{level="info"} {"level": "info", "method": "POST", "path": "/", "host": "grafana.net", "status": "200"}
```

For the query `{job="varlogs"}|json|keep level, tenant, app=~"some-api.*"`, with the following log lines:

```
{"app": "some-api-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
{"app": "other-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
```

the result will be

```
{app="some-api-service", level="info"} {"app": "some-api-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
{level="info"} {"app": "other-service", "level": "info", "method": "GET", "path": "/", "host": "grafana.net", "status": "200"}
```

