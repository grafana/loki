# LogQL Fields

- Author: Cyril Tovena (@cyriltovena)
- Date: Mar 18 2020
- Status: DRAFT

This design document is an extension of [the primer design document](https://docs.google.com/document/d/1BrxnzqEfZnVVYUnjpZR-b9i5w9FNWRzzhyOq5ftCc-Q/edit#heading=h.2obxic2dx0xn) for extracting and analysing data from logs using Loki's LogQL.

Based on the feedback and our own usage, it’s pretty clear that we want to support extracting fields out of the log line for detailed analysis. [Amazon Cloud Watch is a good example](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_QuerySyntax-examples.html) of such language features.

Some users are currently extracting labels using promtail pipeline stages as a way to filter and aggregate logs but this causes issues with our storage system. (Causes the creation of a lot of small chunks, putting pressure on the filesystem when trying to aggregated over a long period of time).

In this proposal I want to introduce multiple new stages into the LogQL language, that will allow to transform a log stream into a field stream which can be then aggregated, compared, sorted and outputed by our users.

## LogQL Parsers

A parser stage can only be applied to log streams (a set of entries attached to a given set of label pairs) and will transform them into field streams.

Field names must be declared by the parser stage and can't conflict with label name, this is mainly to avoid confusion when aggregating. (However this means we might detect the problem once we have already started processing logs, if this causes issues we could later choose that fields have precendence over labels.)

To cover a wide range of structured and unstructured logs, we should start with those four parsers: `glob`, `logfmt`, `json` and `regexp`.

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

We could support one of those notations:

```logql
{job="prod/app"} |json .route client_ip=.client.ip
{job="prod/app"} |json .route .client.ip as route, client_ip
```

the log line :

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
For intance `{job="prod/app"} |logfmt ip=client_ip path` will rename the field `client_ip` into `ip`

### regexp

At last, the regexp should cover all other use cases but is probably the less efficient and intuitive.

While we already support `|~` and `!~` filters, we don't want to mix parsers and filters since their purpose are different, filters should be used to narrow the log volume and reduce the number of lines to parse. This is why I would recommend that we use the `|regexp` operator.

For example the expression `{job="prod/app"} |regexp "(?<client_ip>.+) - - (?<path>.+)"` will transform the log line `127.0.0.1 - - /foo/bar` into :

```json
{
    "client_ip": "127.0.0.1",
    "path": "/foo/bar"
}
```

### Questions

- Should we allow to infer field names such as `{job="prod/app"} |logfmt` and `{job="prod/app"} |json` would extract all fields by their name ?
- In the previous document the `as field_name1, field_name2` field renaming notation was also interesting should we use is over `ip=client_ip` ?

## LogQL Fields Operation

### Filter

### Sort

### Select

### Metrics

TBD

## API Changes

### Faceting

## Metrics
