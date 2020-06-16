# LogQL: Log Query Language

Loki comes with its own PromQL-inspired language for queries called *LogQL*.
LogQL can be considered a distributed `grep` that aggregates log sources,
LogQL uses labels and operators for filtering.

There are two types of LogQL queries: 

-*Log queries* return the contents of log lines.
-*Metric queries* extend log queries and calculate values based on the counts of logs from a log query.

A basic log query consists of two parts: 
-**log stream selector**
-**filter expression**
Due to Loki's design, all LogQL queries must contain a log stream selector.

The log stream selector determines how many log streams (unique sources of log
content, such as files) will be searched. A more granular log stream selector then
reduces the number of searched streams to a manageable volume. This means that
the labels passed to the log stream selector will affect the relative
performance of the query's execution. The filter expression is then used to do a
distributed `grep` over the aggregated logs from the matching log streams.

> To avoid escaping special characters you can use the `` ` ``(back-tick) instead of `"` when quoting strings.
For example `` `\w+` `` is the same as `"\\w+"`. This is specially useful when writing a regular expression which contains multiple backslashes that require escaping.

### Log Stream Selector

The log stream selector determines which log streams should be included in your
query results. The stream selector is comprised of one or more key-value pairs,
where each key is a **log label** and the value is that label's value.

The log stream selector is written by wrapping the key-value pairs in a pair of
curly braces:

```
{app="mysql",name="mysql-backup"}
```

In this example, all log streams that have a label of `app` whose value is `mysql`
_and_ a label of `name` whose value is `mysql-backup` will be included in the
query results. Note that this will match any log stream whose labels _at least_
contain `mysql-backup` for their name label; if there are multiple streams that
contain that label, logs from all of the matching streams will be shown in the
results.

The `=` operator after the label name is a **label matching operator**. The
following label matching operators are supported:

- `=`: exactly equal.
- `!=`: not equal.
- `=~`: regex matches.
- `!~`: regex does not match.

Examples:

- `{name=~"mysql.+"}`
- `{name!~"mysql.+"}`
- `` {name!~`mysql-\d+`} ``

The same rules that apply for [Prometheus Label
Selectors](https://prometheus.io/docs/prometheus/latest/querying/basics/#instant-vector-selectors)
apply for Loki log stream selectors.

### Filter Expression

After writing the log stream selector, the resulting set of logs can be further filtered with a search expression. The search expression can be just text or regex:

- `{job="mysql"} |= "error"`
- `{name="kafka"} |~ "tsdb-ops.*io:2003"`
- `` {name="cassandra"} |~  `error=\w+` ``
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

## Metric Queries

LogQL also supports wrapping a log query with functions that allows for counting
entries per stream.

Metric queries can be used to calculate things such as the rate of error
messages, or the top N log sources with the most amount of logs over the last 3
hours.

### Range Vector aggregation

LogQL shares the same [range vector](https://prometheus.io/docs/prometheus/latest/querying/basics/#range-vector-selectors)
concept from Prometheus, except the selected range of samples include a value of
1 for each log entry. An aggregation can be applied over the selected range to
transform it into an instance vector.

The currently supported functions for operating over are:

- `rate`: calculates the number of entries per second
- `count_over_time`: counts the entries for each log stream within the given
  range.
- `bytes_rate`: calculates the number of bytes per second for each stream.
- `bytes_over_time`: counts the amount of bytes used by each log stream for a given range.

> `count_over_time({job="mysql"}[5m])`

This example counts all the log lines within the last five minutes for the
MySQL job.

> `rate({job="mysql"} |= "error" != "timeout" [10s] )`

This example demonstrates that a fully LogQL query can be wrapped in the
aggregation syntax, including filter expressions. This example gets the
per-second rate of all non-timeout errors within the last ten seconds for the
MySQL job.

It should be noted that the range notation `[5m]` can be placed at end of the log stream filter or right after the log stream matcher. For example, the two syntaxes below are equivalent:

```logql
rate({job="mysql"} |= "error" != "timeout" [5m])

rate({job="mysql"}[5m] |= "error" != "timeout")
```

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

The `without` clause removes the listed labels from the resulting vector, keeping
all others. The `by` clause does the opposite, dropping labels that are not
listed in the clause, even if their label values are identical between all
elements of the vector.

#### Examples

Get the top 10 applications by the highest log throughput:

> `topk(10,sum(rate({region="us-east1"}[5m])) by (name))`

Get the count of logs for the last five minutes, grouping
by level:

> `sum(count_over_time({job="mysql"}[5m])) by (level)`

Get the rate of HTTP GET requests from NGINX logs:

> `avg(rate(({job="nginx"} |= "GET")[10s])) by (region)`

### Binary Operators

#### Arithmetic Binary Operators

Arithmetic binary operators
The following binary arithmetic operators exist in Loki:

- `+` (addition)
- `-` (subtraction)
- `*` (multiplication)
- `/` (division)
- `%` (modulo)
- `^` (power/exponentiation)

Binary arithmetic operators are defined between two literals (scalars), a literal and a vector, and two vectors.

Between two literals, the behavior is obvious: they evaluate to another literal that is the result of the operator applied to both scalar operands (1 + 1 = 2).

Between a vector and a literal, the operator is applied to the value of every data sample in the vector. E.g. if a time series vector is multiplied by 2, the result is another vector in which every sample value of the original vector is multiplied by 2.

Between two vectors, a binary arithmetic operator is applied to each entry in the left-hand side vector and its matching element in the right-hand vector. The result is propagated into the result vector with the grouping labels becoming the output label set. Entries for which no matching entry in the right-hand vector can be found are not part of the result.

##### Examples

Implement a health check with a simple query:

> `1 + 1`

Double the rate of a a log stream's entries:

> `sum(rate({app="foo"})) * 2`

Get proportion of warning logs to error logs for the `foo` app

> `sum(rate({app="foo", level="warn"}[1m])) / sum(rate({app="foo", level="error"}[1m]))`

Operators on the same precedence level are left-associative (queries substituted with numbers here for simplicity). For example, 2 * 3 % 2 is equivalent to (2 * 3) % 2. However, some operators have different priorities: 1 + 2 / 3 will still be 1 + ( 2 / 3 ). These function identically to mathematical conventions.


#### Logical/set binary operators

These logical/set binary operators are only defined between two vectors:

- `and` (intersection)
- `or` (union)
- `unless` (complement)

`vector1 and vector2` results in a vector consisting of the elements of vector1 for which there are elements in vector2 with exactly matching label sets. Other elements are dropped.

`vector1 or vector2` results in a vector that contains all original elements (label sets + values) of vector1 and additionally all elements of vector2 which do not have matching label sets in vector1.

`vector1 unless vector2` results in a vector consisting of the elements of vector1 for which there are no elements in vector2 with exactly matching label sets. All matching elements in both vectors are dropped.

##### Examples

This contrived query will return the intersection of these queries, effectively `rate({app="bar"})`

> `rate({app=~"foo|bar"}[1m]) and rate({app="bar"}[1m])`

#### Comparison operators

-  `==` (equality)
-  `!=` (inequality)
-  `>` (greater than)
-  `>=` (greater than or equal to)
-  `<` (less than)
-  `<=` (less than or equal to)

Comparison operators are defined between scalar/scalar, vector/scalar, and vector/vector value pairs. By default they filter. Their behavior can be modified by providing bool after the operator, which will return 0 or 1 for the value rather than filtering.

Between two scalars, the bool modifier must be provided and these operators result in another scalar that is either 0 (false) or 1 (true), depending on the comparison result.

> `1 >= 1` is equivalent to `1`

Between a vector and a scalar, these operators are applied to the value of every data sample in the vector, and vector elements between which the comparison result is false get dropped from the result vector. If the bool modifier is provided, vector elements that would be dropped instead have the value 0 and vector elements that would be kept have the value 1.

>  `count_over_time({foo="bar"}[1m]) > 10` Filters the streams which log at elast 10 lines in the last minute.

>  `count_over_time({foo="bar"}[1m]) > bool 10` The same as above, but instead of filtering, attached the value 0 to streams that log less than 10 lines.

Between two vectors, these operators behave as a filter by default, applied to matching entries. Vector elements for which the expression is not true or which do not find a match on the other side of the expression get dropped from the result, while the others are propagated into a result vector. If the bool modifier is provided, vector elements that would have been dropped instead have the value 0 and vector elements that would be kept have the value 1, with the grouping labels again becoming the output label set.

> `sum without(app) (count_over_time({app="foo"}[1m])) > sum without(app) (count_over_time({app="bar"}[1m]))` Returns the streams matching `app=foo` without app labels that have higher counts within the last minute than their counterparts matching `app=bar`without app labels.

> `sum without(app) (count_over_time({app="foo"}[1m])) > bool sum without(app) (count_over_time({app="bar"}[1m]))` The same as above, but vectors have their values set to 1 if they pass the comparison or 0 if they fail/would otherwise have been filtered out.
