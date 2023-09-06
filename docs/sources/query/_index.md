---
title: "LogQL: Log query language"
menuTItle: Query
description: LogQL, Loki's query language for logs.
aliases: 
- ./logql
weight: 600
---

# LogQL: Log query language

LogQL is Grafana Loki's PromQL-inspired query language.
Queries act as if they are a distributed `grep` to aggregate log sources.
LogQL uses labels and operators for filtering.

There are two types of LogQL queries:

- [Log queries]({{< relref "./log_queries" >}}) return the contents of log lines.
- [Metric queries]({{< relref "./metric_queries" >}}) extend log queries to calculate values
based on query results.
