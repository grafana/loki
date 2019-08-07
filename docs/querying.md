# Querying

To get the previously ingested logs back from Loki for analysis, you need a
client that supports LogQL.  
Grafana will be the first choice for most users,
nevertheless [LogCLI](logcli.md) represents a viable standalone alternative.

## Clients
### Grafana

Grafana ships with built-in support for Loki for versions greater than
[6.0](https://grafana.com/grafana/download/6.0.0), however using
[6.3](https://grafana.com/grafana/download/6.3.0) or later is highly
recommended.

1. Log into your Grafana, e.g, `http://localhost:3000` (default username:
   `admin`, default password: `admin`)
2. Go to `Configuration` > `Data Sources` via the cog icon on the left side bar.
3. Click the big <kbd>+ Add data source</kbd> button.
4. Choose Loki from the list.
5. The http URL field should be the address of your Loki server e.g.
   `http://localhost:3100` when running locally or with docker,
   `http://loki:3100` when running with docker-compose or kubernetes.
6. To see the logs, click <kbd>Explore</kbd> on the sidebar, select the Loki
   datasource, and then choose a log stream using the <kbd>Log labels</kbd>
   button.

Read more about the Explore feature in the [Grafana
docs](http://docs.grafana.org/features/explore) and on how to search and filter
logs with Loki.

> To configure the datasource via provisioning see [Configuring Grafana via
> Provisioning](http://docs.grafana.org/features/datasources/loki/#configure-the-datasource-with-provisioning)
> and make sure to adjust the URL similarly as shown above.

### LogCLI
If you prefer a command line interface, [LogCLI](logcli.md) also allows to run
LogQL queries against a Loki server. Refer to its [documentation](logcli.md) for
more details.

## LogQL
Loki has it's very own language for querying logs from the Loki server called *LogQL*. Think of
it as distributed `grep` with labels for selection.

A log query consists of two parts: **log stream selector**, and a **filter
expression**. For performance reasons you need to start by choosing a set of log
streams using a Prometheus-style log stream selector.

The log stream selector will reduce the number of log streams to a manageable
volume and then the regex search expression is used to do a distributed grep
over those log streams.

### Log Stream Selector

For the label part of the query expression, wrap it in curly braces `{}` and
then use the key value syntax for selecting labels. Multiple label expressions
are separated by a comma:

`{app="mysql",name="mysql-backup"}`

The following label matching operators are currently supported:

- `=` exactly equal.
- `!=` not equal.
- `=~` regex-match.
- `!~` do not regex-match.

Examples:

- `{name=~"mysql.+"}`
- `{name!~"mysql.+"}`

The same rules that apply for [Prometheus Label
Selectors](https://prometheus.io/docs/prometheus/latest/querying/basics/#instant-vector-selectors)
apply for Loki Log Stream Selectors.

### Filter Expression

After writing the Log Stream Selector, you can filter the results further by
writing a search expression. The search expression can be just text or a regex
expression.

Example queries:

- `{job="mysql"} |= "error"`
- `{name="kafka"} |~ "tsdb-ops.*io:2003"`
- `{instance=~"kafka-[23]",name="kafka"} != kafka.server:type=ReplicaManager`

Filter operators can be chained and will sequentially filter down the
expression - resulting log lines will satisfy _every_ filter. Eg:

`{job="mysql"} |= "error" != "timeout"`

The following filter types have been implemented:

- `|=` line contains string.
- `!=` line does not contain string.
- `|~` line matches regular expression.
- `!~` line does not match regular expression.

The regex expression accepts [RE2
syntax](https://github.com/google/re2/wiki/Syntax). The matching is
case-sensitive by default and can be switched to case-insensitive prefixing the
regex with `(?i)`.

### Query Language Extensions

The query language is still under development to support more features, e.g.,:

- `AND` / `NOT` operators
- Number extraction for timeseries based on number in log messages
- JSON accessors for filtering of JSON-structured logs
- Context (like `grep -C n`)
