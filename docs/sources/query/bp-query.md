---
title: Query best practices
menuTitle:  Query best practices
description: Describes best practices for querying in Grafana Loki.
aliases:
- ../bp-query
weight: 700
---
# Query best practices

The way you write queries in Loki affects how quickly you get results returned from those queries. Understanding the way Loki parses queries can help you write queries that are efficient and performant.

{{< admonition type="tip" >}}
Before you start optimizing queries, read the [labels best practices]({{< relref "../get-started/labels/bp-labels" >}}) page to understand what makes a good label. Choosing the right labels in the first place is the first step towards writing efficient queries.
{{< /admonition >}}

Loki evaluates a LogQL query from left to right, in the order that it is written. Your goal is to eliminate as many potential results as you can earlier in the query, and then continue to progressively narrow your search as you continue writing the query. Below is the recommended order for how to filter efficiently in your query.

## Narrow down your time range first

- Start with a time frame.  What is the time range for the data that you’re looking for?
	- One index file per day, multi day queries fetch multiple index files.
	- Within that Loki looks at the label matchers

## Use precise label selectors


- Next write your Label selectors
	- Use most specific label that you have
	- (address, don’t start with country code, start with street address)
	- You don’t really gain anything by adding more labels (it doesn’t hurt either), use the most specific label you can.

## Use simple line filters

- Filter expressions (Pipe equals, or exclamation equals, must contain, does not contain)  This is the next fastest thing that Loki can evaluate.  If the log line does not match the filter expression, Loki can stop processing it and save the effort of the parsing step.
	- Filter before parsing
	- Prefer pipe filters over regular expressions.

## Avoid using complex text parsers

(logfmt, json, regex)


## Use recording rules for frequently used metrics


[recording rules]({{< relref "../operations/recording-rules/" >}})
## Resources

- [Watch: 5 tips for improving Grafana Loki query performance](https://grafana.com/blog/2023/01/10/watch-5-tips-for-improving-grafana-loki-query-performance/)
- [Grafana Loki Design Basics with Ed Welch (Grafana Office Hours #27)](https://www.youtube.com/live/3uFMJLufgSo?feature=shared&t=3385)
- 