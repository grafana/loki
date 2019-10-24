# LogQL: Log Query Language

Loki comes with its very own language for querying logs called *LogQL*. LogQL
can be considered a distributed `grep` with labels for filtering.

A basic LogQL query consists of two parts: the **log stream selector** and a
**filter expression**. Due to Loki's design, all LogQL queries are required to
contain a log stream selector.

The log stream selector will reduce the number of log streams to a manageable
volume. Depending how many labels you use to filter down the log streams will
affect the relative performance of the query's execution. The filter expression
is then used to do a distributed `grep` over the retrieved log streams.

### Log Stream Selector

The log stream selector determines which log streams should be included in your
query. The stream selector is comprised of one or more key-value pairs, where
each key is a **log label** and the value is that label's value.

The log stream selector is written by wrapping the key-value pairs in a
pair of curly braces:

```
{app="mysql",name="mysql-backup"}
```

In this example, log streams that have a label of `app` whose value is `mysql`
_and_ a label of `name` whose value is `mysql-backup` will be included in the
query results.

The `=` operator after the label name is a **label matching operator**. The
following label matching operators are supported:

- `=`: exactly equal.
- `!=`: not equal.
- `=~`: regex matches.
- `!~`: regex does not match.

Examples:

- `{name=~"mysql.+"}`
- `{name!~"mysql.+"}`

The same rules that apply for [Prometheus Label
Selectors](https://prometheus.io/docs/prometheus/latest/querying/basics/#instant-vector-selectors)
apply for Loki log stream selectors.

### Filter Expression

After writing the log stream selector, the resulting set of logs can be filtered
further with a search expression. The search expression can be just text or
regex:

- `{job="mysql"} |= "error"`
- `{name="kafka"} |~ "tsdb-ops.*io:2003"`
- `{instance=~"kafka-[23]",name="kafka"} != kafka.server:type=ReplicaManager`

In the previous examples, `|=`, `|~`, and `!=` act as **filter operators** and
the following filter operators are supported:

- `|=`: Log line contains string.
- `!=`: Log line does not contain string.
- `|~`: Log line matches regular expression.
- `!~`: Log line does not match regular expression.

Filter operators can be chained and will sequentially filter down the
expression - resulting log lines must satisfy _every_ filter:

`{job="mysql"} |= "error" != "timeout"`

When using `|~` and `!~`,
[Go RE2 syntax](https://github.com/google/re2/wiki/Syntax) regex may be used. The
matching is case-sensitive by default and can be switched to case-insensitive
prefixing the regex with `(?i)`.

## Counting logs

LogQL also supports functions that wrap a query and allow for counting entries
per stream.

### Range Vector aggregation

LogQL shares the same [range vector](https://prometheus.io/docs/prometheus/latest/querying/basics/#range-vector-selectors)
concept from Prometheus, except the selected range of samples include a value of
1 for each log entry. An aggregation can be applied over the selected range to
transform it into an instance vector.

The currently supported functions for operating over are:

- `rate`: calculate the number of entries per second
- `count_over_time`: counts the entries for each log stream within the given
  range.

> `count_over_time({job="mysql"}[5m])`

This example counts all the log lines within the last five minutes for the
MySQL job.

> `rate( ( {job="mysql"} |= "error" != "timeout)[10s] ) )`

This example demonstrates that a fully LogQL query can be wrapped in the
aggregation syntax, including filter expressions. This example gets the
per-second rate of all non-timeout errors within the last ten seconds for the
MySQL job.

### Aggregation operators

Like [PromQL](https://prometheus.io/docs/prometheus/latest/querying/operators/#aggregation-operators),
LogQL supports a subset of built-in aggregation operators that can be used to
aggregate the element of a single vector, resulting in a new vector of fewer
elements but with aggregated values:

- `sum`: Calculate sum over labels
- `min`: Select minimum over labels
- `max`: Select maximum over labels
- `avg`: Calculate the average over labels
- `stddev`: Calculate the population standard deviation over labels
- `stdvar`: Calculate the population standard variance over labels
- `count`: Count number of elements in the vector
- `bottomk`: Select smallest k elements by sample value
- `topk`: Select largest k elements by sample value

The aggregation operators can either be used to aggregate over all label
values or a set of distinct label values by including a `without` or a
`by` clause:

> `<aggr-op>([parameter,] <vector expression>) [without|by (<label list>)]`

`parameter` is only required when using `topk` and `bottomk`. `topk` and
`bottomk` are different from other aggregators in that a subset of the input
samples, including the original labels, are returned in the result vector. `by`
and `without` are only used to group the input vector.

The `without` cause removes the listed labels from the resulting vector, keeping
all others. The `by` clause does the opposite, dropping labels that are not
listed in the clause, even if their label values are identical between all
elements of the vector.

#### Examples

Get the top 10 applications by the highest log throughput:

> `topk(10,sum(rate({region="us-east1"}[5m]) by (name))`

Get the count of logs during the last five minutes, grouping
by level:

> `sum(count_over_time({job="mysql"}[5m])) by (level)`

Get the rate of HTTP GET requests from NGINX logs:

> `avg(rate(({job="nginx"} |= "GET")[10s])) by (region)`
