# LogQL Fields

- Author: Cyril Tovena (@cyriltovena)
- Date: Mar 18 2020
- Status: DRAFT

This design document is an extension of [the first design document](https://docs.google.com/document/d/1BrxnzqEfZnVVYUnjpZR-b9i5w9FNWRzzhyOq5ftCc-Q/edit#heading=h.2obxic2dx0xn) for extracting and analyzing data from logs using Loki's LogQL.

Based on the feedback of the first design and our own usage, it’s pretty clear that we want to support extracting fields out of the log line for detailed analysis. [Amazon Cloud Watch is a good example](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_QuerySyntax-examples.html) of such language features.

For instance some users are currently extracting labels using Promtail pipeline stages as a way to filter and aggregate logs but this causes issues with our storage system. (Causes the creation of a lot of small chunks, putting pressure on the filesystem when trying to aggregated over a long period of time).

In this proposal I want to introduce multiple new operators into the LogQL language, that will allow to transform a log stream into a field stream which can be then aggregated, compared, sorted and outputted by our users.

You'll see that all those new operators (except aggregations over time) are chain together with the `|` pipe operator. This is because we believe it's the most intuitive way to write queries that process data in the Linux world as opposed to the function format. (e.g `cat my.log | grep -e 'foo' | jq .field | wc -l` is easier to read than `wc(jq(grep(cat my.log,'foo'),.field),-l)`)

## Table of Content

<!-- TOC -->

- [LogQL Fields](#logql-fields)
    - [Table of Content](#table-of-content)
    - [Related Documents](#related-documents)
    - [LogQL Parsers](#logql-parsers)
        - [Glob](#glob)
        - [Json](#json)
        - [Logfmt](#logfmt)
        - [Regexp](#regexp)
        - [Questions](#questions)
    - [LogQL Fields Operators](#logql-fields-operators)
        - [Filter](#filter)
        - [Sort](#sort)
        - [Select](#select)
        - [Limit](#limit)
        - [Uniq](#uniq)
        - [Metrics](#metrics)
            - [Series & Histogram Operators](#series--histogram-operators)
            - [Range Vector Operations](#range-vector-operations)
    - [API Changes](#api-changes)
        - [Faceting](#faceting)
    - [Glossary](#glossary)
    - [Syntax Types](#syntax-types)

<!-- /TOC -->

## Related Documents

- [LogQL Pseudo Label Extraction Design Doc](https://docs.google.com/document/d/1BrxnzqEfZnVVYUnjpZR-b9i5w9FNWRzzhyOq5ftCc-Q/edit#)
- [Original LogQL Design Doc](https://docs.google.com/document/d/1DAzwoZF0yD4oFSFcUlRcGG0Ent2myAlFr_lZc6es3fs/edit#heading=h.6u7w5ql6hn4p)
- [Current LogQL Documentation](https://github.com/grafana/loki/blob/master/docs/logql.md)

## LogQL Parsers

A parser operator can only be applied to __log__ streams (a set of entries attached to a given set of label pairs) and will transform (or extract) them into __field__ streams.

As quick overview let's consider stream below:

```json
{
  "stream": {
   "job": "loki-dev/ingester",
   "cluster": "us-central1",
   "namespace" : "loki-dev",
  },
  "values": [
    [
      "1584114643856675277",
      "level=info ts=2020-03-13T15:50:43.856555 caller=metrics.go"
    ],
    [
      "1584114643856675278",
      "level=warn ts=2020-03-13T15:50:43.856556 caller=metrics.go"
    ]
  ]
}
```

As you can see the log is using the popular logfmt so we could use the `| logfmt` parsers to transform those entries into the fields entries like below:

```json
{
  "stream": {
   "job": "loki-dev/ingester",
   "cluster": "us-central1",
   "namespace" : "loki-dev",
  },
  "values": [
    [
      "1584114643856675277",
      {
          "level":"info",
          "ts": "2020-03-13T15:50:43.856555",
          "caller": "metrics.go"
      }
    ],
   [
      "1584114643856675278",
      {
          "level":"warn",
          "ts": "2020-03-13T15:50:43.856556",
          "caller": "metrics.go"
      }
    ]
  ]
}
```

Field names are declared by each parsers, some parser will allow implicit fields extraction for __brevity__ like the `json` and `logfmt` parsers.

Since field name can conflicts with labels during aggregation we will support for each parser the ability to rename a field. However in case of conflict fields will take precedence over labels.

> Implicit fields extraction could means trouble in term of performance. However in most cases we will be able to infer all used fields from the full query and so limit the extraction per line.

To cover a wide range of structured and unstructured logs, we will start with those four parsers: `glob`, `logfmt`, `json` and `regexp`.

### Glob

The glob parser (as described in previous design) `{job="prod/nginx"} | glob "*<client_ip> - - [ * ] \"* *<path> "` could be used to extract fields from nginx logs.

The log entry below:

```log
127.0.0.1 - - [19/Jun/2012:09:16:22 +0100] "GET /GO.jpg HTTP/1.1" 499 0 "http://domain.com/htm_data/7/1206/758536.html" "Mozilla/4.0 (compatible; MSIE 7.0; Windows NT 5.1; Trident/4.0; .NET CLR 1.1.4322; .NET CLR 2.0.50727; .NET CLR 3.0.4506.2152; .NET CLR 3.5.30729; SE 2.X MetaSr 1.0)"
```

would be transformed into those fields:

```json
{
    "client_ip": "127.0.0.1",
    "path": "/GO.jpg"
}
```

This parser use [glob pattern matching](https://en.wikipedia.org/wiki/Glob_(programming)) and extract named matches(*\<name>) into fields.
If the line doesn't match the glob it will be filtered out.

### Json

The json parser allows to parse a json log line and select fields to extract. If the line is not a valid json document the line will be skipped.

We could support those notations:

```logql
{job="prod/app"} | json .route client_ip=.client.ip
{job="prod/app"} | json .route .client.ip as route, client_ip
{job="prod/app"} | json |select route client_ip
{job="prod/app"} | json
```

> `{job="prod/app"} |json |select route client_ip` is an implicit extraction, the list of extracted field is inferred from the full query.

The log line :

```log
{
    "route": "/api/v1/foo",
    "client": {
        "ip": "127.0.0.1",
        "org-id": "grafana-labs"
    }
}
```

would be transformed into those fields:

```json
{
    "client_ip": "127.0.0.1",
    "route": "/api/v1/foo"
}
```

### Logfmt

The logfmt parser will allow to extract fields from the [log format](https://brandur.org/logfmt). For example the expression `{job="prod/app"} | logfmt client_ip path` will transform the log line `time=03-10-2020T20:00:01 path=/foo/bar client_ip="127.0.0.1"` into :

```json
{
    "client_ip": "127.0.0.1",
    "path": "/foo/bar"
}
```

Again it might be useful to allow renaming fields to avoid conflicts with labels.

For instance `{job="prod/app"} | logfmt ip=client_ip path` will rename the field `client_ip` into `ip`.

`logfmt` will also support implicit field extraction like `json`.

### Regexp

At last, the regexp operator should cover all other use cases but is probably the less efficient and intuitive.

> While we already support `|~` and `!~` filters, we don't want to mix parsers and filters since their purpose are different, filters should be used to narrow the log volume and reduce the number of lines to parse. This is why we will use the notation `| regexp` for the parser operator.

For example the expression `{job="prod/app"} | regexp "(?<client_ip>.+) - - (?<path>.+)"` will transform the log line `127.0.0.1 - - /foo/bar` into :

```json
{
    "client_ip": "127.0.0.1",
    "path": "/foo/bar"
}
```

### Questions

- Should we allow to implicit field extraction such as `{job="prod/app"} | logfmt` and `{job="prod/app"} | json` would extract all fields ? I really like the simplicity of it.
- In the previous document the `as field_name1, field_name2` field renaming notation was also interesting should we use is over `ip=client_ip` for now I've opted for the later which can be omitted in case of no renaming ?
- Do we prefer `|json` or `| json` notation ?

## LogQL Fields Operators

Now that we have transformed our log streams into log fields we can pipe/chain a new set of operators.
(Some operators are also compatible with log stream such as the `limit` and `uniq` operators.)

For example the query below:

```logql
{job="prod/query-frontend"} | logfmt | filter level="info" and latency > 10 | sort duration desc | limit 25 | select query duration
```

returns the first 25 query and duration slower than 10s ordered by the slowest in the requested time range.

Let's have a look in details how those new operators work.

### Filter

A filter operation noted `| filter` can filter out fields by their value using test predicates such as `path = "/foo/bar"`.

Multiple test predicates can be chained/combined together using `and` and `or` logic operations.

At least one leg of the test predicate must be a literal, this is to ensure we can infer the type to use for comparison. We could later add support for cross field comparison.

__String literal__ will support the same Prometheus match type. (`=`,`=!`,`!~`,`=~`).

Example: `{job="prod/query-frontend"} | logfmt | filter level=~"info|debug" and query_type!="filter"`

__Numeric literal__ will support `=`,`!=` but also `<`,`<=`,`>`,`>=`.

Example: `{job="prod/query-frontend"} | json | filter latency > 10 or status >= 400`

When field value and literal types are different, conversion will be attempted prior testing and will be translated into zero type values in case of failure.

In the future we will add more literals such as durations `2s` and dates (`03-03-2020T20:30`).

### Sort

The sort operation noted `| sort` allows to orders fields entries by a field value instead of the field entry time.

> It should be noted that sorting is always executed after parsing fields entries in the time range. Not on the whole time range.

For example: `{job="prod/query-frontend"} | json | sort latency desc`

will sort  the first 1000 (default limit) fields entries by the highest latency first. Multiple fields can be used as fallback by the sort operation in case of equality.

For example: `{job="prod/query-frontend"} | json | sort latency desc path asc` will sort by latency descending first then path ascending.

Using sort operator with metric queries will result into an error.(e.g `count_over_time({job="prod/query-frontend"} | logfmt  | sort latency desc [5m])`)

> __Warning:__ Sorting is dependent on how Grafana is currently ordering lines. Even if we re order our responses there is no guarantee that Grafana will be relying on it. And we might have to remove uncommon labels to keep only stream to ensure ordering across field streams. This means we will need a new API (`/v2`) to support ordering, although with compromise we might get something to work with the `v1` API.

### Select

The Select operator (`| select`) allows you to select and reduce field that should be returned.

If you don't provide a select operator, all fields parsed will be returned by default. This means all fields from a log line if you're using implicit extraction. However if you used explicit extraction, only those fields will be returned.

For example this query:

```logql
{job="prod/query-frontend"} | logfmt | sort duration desc | limit 25 | select query duration
```

is the same as this one

```logql
{job="prod/query-frontend"} | logfmt query duration | sort duration desc | limit 25
```

because the field extraction was explicit, only those fields (`query` and `duration`) are outputted.

This is why select is important to use for large queries. Select will allows users to remove data (column) in their logs that are useless, repetitive and not interesting for the current investigation.

The select operation will also support the [go templating language](https://golang.org/pkg/text/template/) when supplied with string argument and not fields.

Fields extracted will be available under the `.FieldName` and `$FieldName` notation in the template.

This way users can format each field to be more digestible. Returning a payload size  field with value of `12321421230123` is less meaningful than `12TB`.

Example:

```logql
{job="prod/query-frontend"} | logfmt | select payload_size="{{ .size | humanize }}" level="{{ $level | ToUpper}}"
```

Renaming is allowed with or without templates, and is optional. An autogenerated field name will be used if it can't be inferred. (`field1`,`field2`, etc...)

__In this current stance, select is not greedy meaning if you want to rename or template a single field, you'll have to declare all other fields to keep them.__

In the future we might consider a `without` operator to take all fields but those passed as parameter. (`{job="prod/query-frontend"} | logfmt | without msg`)

### Limit

The limit operator (`| limit x`) is a new operator to overwrite the query string limit, this can be handy for building dashboard with different limits but also when writing and testing queries, you could start to experiment with a low limit to increase the feedback loop.

If the limit operator is missing in the expression, the limit from the query string is used.

The limit operator can be applied to a log field  as well as a log stream.

For example all of the above are corrects.

```logql
{job="prod/query-frontend"} | logfmt query duration | sort duration desc | limit 25
{job="prod/query-frontend"} | limit 10
{job="prod/query-frontend"} |= "foo" | limit 20
```

Using limit operator with metric queries will result into an error. (e.g `rate({job="prod/query-frontend"} | limit 10[5m])`)

### Uniq

The uniq operator allows to filter out duplicates. Without any parameter (`| uniq`) each fields combination must be unique from one entry to another. However
the filtering can be forced on a specific field, for example `| uniq status_code`, this will returns one entry per status code.

Like the limit operator, the unique operator is allowed on log stream without parameters such as `{job="prod/query-frontend"} |= "err" | uniq` will returns the first 1000 unique lines containing the `err` word.
This is really useful to reduce noise applications might generate by failing constantly.

Using unique operator with metric queries will result into an error.(e.g `rate({job="prod/query-frontend"} | uniq [5m])`).

### Metrics

The first interesting change will be that `count_over_time` and `rate` is now able to count field or log entries. Fields can also be used like labels during metric aggregation.

For example you can now aggregate by level without indexing it using the query below:

```logql
rate({job="prod/query-frontend"} | logfmt level [1m])
```

This will automatically add the level fields as part of the series for each series. Again you should be mindful about which fields you select as this will increase the amount of series returned.

Fields can also be used during dimension aggregation like  labels:

```logql
sum (
    rate({job="prod/query-frontend"} | logfmt | select path status_code [1m])
) by (instance, status_code, path)

```

Aggregation dimension will be limited, if fields contains too many unique values we will return an error.

While `count_over_time` and `rate` count entries occurrence per log & field stream, now that we can select fields we will allow some new aggregation functions over those field values.

#### Series & Histogram Operators

To transform fields into series we will introduce two new field stream operators `| series` and `| histogram`. In the future we could introduce other operators like `| counter <field> [inc|dec] by (<label>,<field>)` to sum values if needed.

The series operator `| series <field> by (<label>,<field>)` creates a series from a field stream, the first parameter is the field to use as value, this means each field entry (field/timestamp pair) will create a point (value/timestamp pair). You can then use `by` or `without` to group series metric (again you can include labels or field or both) prior aggregations. You can also omit the grouping clause in which case all extracted fields and labels will define the set of series returned.

Example of a series transformation:

```logql
{job="app"} | json | series latency by (path, status)
{job="app"} | json | series latency
{job="app"} | json | series latency without (instance)
```

The histogram operator `| histogram <field> [<array_of_float>] by (<label>,<field>)` creates a [Prometheus histogram](https://prometheus.io/docs/concepts/metric_types/#histogram) with the given `<array_of_float>` buckets. It will compute cumulative counters for the observation buckets as well as a counter of all observed values and the total sum of all observations. Series count per query will also be limited, so trying to use too many buckets will result in an error.

To simplify the creation of buckets we will provide two function syntax to create buckets: `exp(start, factor float, count int)` and `linear(start, width float64, count int)` which will respectively create exponential and linear buckets.

See below the 3 version for declaring buckets of histogram:

```logql
{job="app"} | json | histogram latency [.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10] by (path,status)

{job="app"} | json | histogram size exp(20000, 2, 10) without (instance)

{job="app"} | json | histogram latency linear(.75, 2, 10)
```

Not providing the array of buckets will result in an error.

>We will only support range vector operations of those series and histograms for now like we do for `count_over_time` and `rate`. This is also because they make more sense as they allow you to select the aggregation range over time for each query. In the future we could may be select the last log line as the instant vector value for each step but I have some doubt it is a useful.

#### Range Vector Operations

Those new functions are very similar to prometheus [over time aggregations](https://prometheus.io/docs/prometheus/latest/querying/functions/#aggregation_over_time). What's interesting with those functions is that they allow the user to pick the range of the aggregation for each steps using the `[1m]` notation.

We plan to support the following new operations:

- `avg_over_time(range-vector)`: the average of all values in the specified interval.
- `min_over_time(range-vector)`: the minimum of all values in the specified interval.
- `max_over_time(range-vector)`: the maximum of all values in the specified interval.
- `sum_over_time(range-vector)`: the sum of all values in the specified interval.
- `quantile_over_time(scalar, range-vector)`: the φ-quantile (0 ≤ φ ≤ 1) of the values in the specified interval.
- `stddev_over_time(range-vector)`: the population standard deviation of the values in the specified interval.
- `stdvar_over_time(range-vector)`: the population standard variance of the values in the specified interval.

> `quantile_over_time(scalar, range-vector)` only works if you have a series with the labels(`le`) this is why you need to use `| histogram` operator on the field stream.

Now let's pretend we have received temperatures from censors around the house into Loki like this:

```log
2020-03-20T10:20:30 LivingRoom 20C
2020-03-20T10:21:00 Kitchen 21C
2020-03-20T10:21:15 Kid1 19C
....
```

To get the average temperatures by room in the last 30 minutes we could use the query below:

```logql
avg_over_time({job="censors"} | glob "* *<room> *<temp>C" | series temp by (room) [30m])
```

As another example if we have some nginx logs like below:

```log
127.0.0.1 - - [19/Jun/2012:09:16:22 +0100] "GET /GO.jpg HTTP/1.1" 499 100 "-" "-"
127.0.0.1 - - [19/Jun/2012:09:16:22 +0100] "GET /test.jpg HTTP/1.1" 200 99 "-" "-"
```

We could get the 99th percentile latency by path and status over the last 15 minutes using the query below:

```logql
quantile_over_time(.99, {job="nginx"} | glob "* -- * \"* /*<path> *\" *<status> *<latency> *" | histogram latency linear(.25, 2, 5) by (path, status) [15m])
```

## API Changes

One aspect that is really important from a usage perspective with all those changes is that the users will be playing/fiddling around with those new parsers operators to find the right query. This is specially true for the `regexp` and `glob` parsers.

This means we need to send feedback to ease the creation of those new queries. Unfortunately I don't expect Grafana to support a new response type (fields) from day one and this means we need to find a way to retrofit fields into logs.

My suggestion is that for any (v1) query we will automatically reconvert fields into log using logfmt format. If there is only one field per entry we will use the field value as log value.

Now this will make all proposed operators work as is in Grafana except for the `| sort` operator.

Since we will need a `v2` anyway for the sort operator to work, I think it also make sense toonly include the new fields result type there too.

### Faceting

As explained in the [parsers section](#logql-parsers), some parsers (`json` and `logfmt`) will support implicit extraction. However when the user will want to reduce fields by selecting them, I think it would be useful to provide an API that returns all field names for a given field stream selector and a time range. This way Grafana or any other integrations will be able to provide auto completion for those fields to select.

## Glossary

- sample pair: a float value for a given timestamp.
- series: samples pair attached to a same set of labels.
- instant vector: contains a single sample per series.
- range vector: contains multiple samples per series.
- log entry: a log and a timestamp.
- field entry: a set of fields (an object) and a timestamp.
- log stream: a set of log entries attached to same set of labels.
- field stream: a set of field entries  attached to same set of labels.

## Syntax Types

TDB
