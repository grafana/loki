---
title: Query best practices
menuTitle:  Query best practices
description: Describes best practices for querying in Grafana Loki.
aliases:
- ../bp-query
weight: 100
---
# Query best practices

The way you write queries in Loki affects how quickly you get results returned from those queries. Understanding the way Loki parses queries can help you write queries that are efficient and performant.

{{< admonition type="tip" >}}
Before you start optimizing queries, read the [labels best practices](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/bp-labels/) page to understand what makes a good label. Choosing the right labels is the first step towards writing efficient queries.
{{< /admonition >}}

Loki evaluates a LogQL query from left to right, in the order that it is written. To get the best possible query performance, eliminate as many potential results as you can earlier in the query and then continue to progressively narrow your search as you continue writing the query. This page describes the recommended order for writing queries that efficiently filter out unwanted results.

## Narrow down your time range first

Reduce the number of logs Loki needs to look through by specifying a period of time that you'd like to search through. Loki creates one index file per day, so queries that span over multiple days fetches multiple index files. The fewer files Loki has to search, the faster the query results are returned.

Time ranges are typically not part of the query, but you can set a time range through your visualization tool or through [the Loki API](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/).


### In Grafana

If you're using Loki with Grafana, you can use the dropdown menu on the upper right hand corner of a dashboard to select a time range, either relative (last X hours) or absolute (a specific date and time).

![Screenshot of time selector on Grafana](../grafana-time-range-picker.png "Grafana time interval selector")

### Through Loki API

If you're querying Loki through [the Loki API](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/), you can use the [`query_range` endpoint](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/#query-logs-within-a-range-of-time)
 to add `start` and `end` timestamps for your query as parameters to the HTTP call rather than as part of the query itself.

```bash
http://<loki-instance>/loki/api/v1/query_range?query={job="app"}&start=1633017600000000000&end=1633104000000000000

```


## Use precise label selectors

Next, write your label selectors. Identify the most specific label you can use within the log line and search based on that first. For example, if the logs contain the labels `namespace` and `app_name` and the latter is a smaller subset of data, start your query by selecting based on `app_name`:

```bash
{app_name="carnivorousgreenhouse"}
```

Using the most specific label selector has the added benefit of reducing the length of your query. Since `app_name` is more specific than `namespace`, you don't need to add a selector for `namespace`. Adding more general label selectors has no further effect on the query.


## Use simple line filter expressions over regular expressions

When using [line filter expressions](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#line-filter-expression), prefer the simpler filter operators such as:
- `|=` (contains string) and 
- `!=` (does not contain string)
over the regular expression filter operators:
- `|~` (matches the regular expression)
- `!~` (does not match the regular expression)

Loki evaluates the first two filter expressions faster than it can evaluate regular expressions, so always try to rewrite your query in terms of whether a log line contains or does not contain a certain string. Use regular expressions only as a last resort.

Line filter expressions are more efficient than parser expressions.

## Avoid using complex text parsers

Use [parser expressions](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#parser-expression) only after line filter expressions. Parser expressions are ways to look through the log line and extract labels in different formats, which can be useful but are also more intensive for Loki to do than line filter expressions. Using them after line filter expressions means that Loki only needs to evaluate parser expressions for log lines that match the line filter expression, reducing the amount of logs that Loki needs to search through.

Parser expressions include [JSON](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#json), [logfmt](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#logfmt), [pattern](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#pattern), [regexp](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#regular-expression), and [unpack](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#unpack) parsers.

## Use recording rules

Some queries are sufficiently complex, or some datasets sufficiently large, that there is a limit as to how much query performance can be optimized. If you're following the tips on this page and are still experiencing slow query times, consider creating a [recording rule](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/recording-rules/) for them. A recording rule runs a query at a predetermined time and also precomputes the results of that query, saving those results for faster retrieval later.

## Further resources

- [Watch: 5 tips for improving Grafana Loki query performance](https://grafana.com/blog/2023/01/10/watch-5-tips-for-improving-grafana-loki-query-performance/)
- [Grafana Loki Design Basics with Ed Welch (Grafana Office Hours #27)](https://www.youtube.com/live/3uFMJLufgSo?feature=shared&t=3385)
- [Labels best practices](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/bp-labels/)