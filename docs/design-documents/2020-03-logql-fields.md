# LogQL Fields

- Author: Cyril Tovena (@cyriltovena)
- Date: Mar 18 2020
- Status: DRAFT

This design document is an extension of [the primer design document](https://docs.google.com/document/d/1BrxnzqEfZnVVYUnjpZR-b9i5w9FNWRzzhyOq5ftCc-Q/edit#heading=h.2obxic2dx0xn) for extracting and analyzing data from logs using Loki's LogQL.

Based on the feedback and our own usage, it’s pretty clear that we want to support extracting fields out of the log line for detailed analysis. [Amazon Cloud Watch is a good example](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_QuerySyntax-examples.html) of such language features.

Some users are currently extracting labels using Promtail pipeline stages as a way to filter and aggregate logs but this causes issues with our storage system. (Causes the creation of a lot of small chunks, putting pressure on the filesystem when trying to aggregated over a long period of time).

In this proposal I want to introduce multiple new stages into the LogQL language, that will allow to transform a log stream into a field stream which can be then aggregated, compared, sorted and outputted by our users.

## LogQL Parsers

A parser operator can only be applied to __log__ streams (a set of entries attached to a given set of label pairs) and will transform (extract) them into __field__ streams.

Field names are declared by each parsers, some parser will allow implicit field extraction for brevity like the `json` and `logfmt` parsers. Since field name can conflicts with labels during aggregation we will support for each parser the ability to rename a field. However in case of conflict field will take precedence over labels.

> Implicit field extraction could mean trouble in term of performance. However in most cases we will be able to infer all used fields from the full query and so limit the extraction per line.

To cover a wide range of structured and unstructured logs, we should start with those four parsers: `glob`, `logfmt`, `json` and `regexp`.

For example if we consider the stream below:

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

The `|logfmt` parsers will transform it into the fields stream below:

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

### Glob

The glob parser (as described in previous design) `{job="prod/nginx"} |* "<client_ip>* - - [ * ] \"* <path>* "` could be used to extract fields from nginx logs.

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

If the line doesn't match the glob it will be filtered out.

### Json

The json parser allows to parse a json log line and select fields to extract. If the line is not a valid json document the line will be skipped.

We could support those notations:

```logql
{job="prod/app"} |json .route client_ip=.client.ip
{job="prod/app"} |json .route .client.ip as route, client_ip
{job="prod/app"} |json |select route client_ip
{job="prod/app"} |json
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

the logfmt parser will allow to extract fields from the [log format](https://brandur.org/logfmt). For example the expression `{job="prod/app"} |logfmt client_ip path` will transform the log line `time=03-10-2020T20:00:01 path=/foo/bar client_ip="127.0.0.1"` into :

```json
{
    "client_ip": "127.0.0.1",
    "path": "/foo/bar"
}
```

Again it might be useful to allow renaming fields to avoid conflicts with labels.

For instance `{job="prod/app"} |logfmt ip=client_ip path` will rename the field `client_ip` into `ip`.

`logfmt` will also support implicit field extraction like `json`.

### regexp

At last, the regexp should cover all other use cases but is probably the less efficient and intuitive.

> While we already support `|~` and `!~` filters, we don't want to mix parsers and filters since their purpose are different, filters should be used to narrow the log volume and reduce the number of lines to parse. This is why I would recommend that we use the `|regexp` operator.

For example the expression `{job="prod/app"} |regexp "(?<client_ip>.+) - - (?<path>.+)"` will transform the log line `127.0.0.1 - - /foo/bar` into :

```json
{
    "client_ip": "127.0.0.1",
    "path": "/foo/bar"
}
```

### Questions

- Should we allow to implicit field names such as `{job="prod/app"} |logfmt` and `{job="prod/app"} |json` would extract all fields by their name ?
- In the previous document the `as field_name1, field_name2` field renaming notation was also interesting should we use is over `ip=client_ip` ?
- Do we prefer `|json` or `| json` notation ?

## LogQL Fields Operation

Now that we have transformed our log streams into log fields we can pipe/chain a new set of operators.

For example the query below:

```logql
{job="prod/query-frontend"} |logfmt |filter level="info" |sort duration desc |limit 25 |select query duration
```

returns the first 25 slowest query and duration pairs in the requested time range.

Let's have a look in details how those new operators work.

### Filter

A filter operation noted `|filter` can filter out fields by their value using test predicates such as `path = "/foo/bar"`.

Multiple test predicates can be chained/combined together using `and` and `or` keywords for respectively and and or logic operation.

At least one leg of the test predicate must be a literal, this is to ensure we can infer the type to use for comparison. We could later add support for cross field comparison.

String literal will support the same Prometheus match type. (`=`,`=!`,`!~`,`=~`).

Example: `{job="prod/query-frontend"} |logfmt |filter level=~"info|debug" and query_type!="filter"`

Numeric literal will support `=`,`!=` but also `<`,`<=`,`>`,`>=`.

Example: `{job="prod/query-frontend"} |logfmt |filter latency > 10 or status >= 400`

When field and literal types are different conversion will be attempted prior testing and will be translated into zero type in case of failure.

In the future we will add more literals such as duration `2s` or date (`03-03-2020T20:30`).

### Sort

The sort operation noted `|sort` allows to orders fields entries by a field value instead of the field entry time.

For example: `{job="prod/query-frontend"} |json |sort latency desc`

Will sort fields by the highest latency first. Multiple fields can be used as fallback by the sort operation in case of equality.

For example: `{job="prod/query-frontend"} |json |sort latency desc path asc` will sort by latency descending first then path ascending.

### Select

The Select operator (`|select`) allows you to select and reduce field that should be returned.

__It should be noted that when using implicit extraction you'll need to end with the query a select if you want to improve performance.__

So this means this query.

```logql
{job="prod/query-frontend"} |logfmt |sort duration desc |limit 25 |select query duration
```

is the same as

```logql
{job="prod/query-frontend"} |logfmt query duration |sort duration desc |limit 25
```

### Metrics

We plan to support more metrics, specially parsing by fields.

TBD

## API Changes

### Faceting

## Performance consideration
