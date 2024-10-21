---
title: Query Acceleration with Blooms (Experimental)
menuTitle: Query Acceleration with Blooms
description: Describes how to enable and configure query acceleration with blooms.
weight:
keywords:
  - blooms
  - query acceleration
---

# Query Acceleration with Blooms (Experimental)
{{% admonition type="warning" %}}
This feature is an [experimental feature](/docs/release-life-cycle/). Engineering and on-call support is not available.  No SLA is provided.
{{% /admonition %}}

Loki leverages [bloom filters](https://en.wikipedia.org/wiki/Bloom_filter) to speed up queries by reducing the amount of data Loki needs to load from the store and iterate through.
Loki is often used to run "needle in a haystack" queries; these are queries where a large number of log lines are searched, but only a few log lines match the query.
Some common use cases are needing to find all logs tied to a specific trace ID or customer ID.

An example of such queries would be looking for a trace ID on a whole cluster for the past 24 hours:

```logql
{cluster="prod"} | traceID="3c0e3dcd33e7"
```

Without accelerated filtering, Loki downloads all the chunks for all the streams matching `{cluster="prod"}` for the last 24 hours and iterates through each log line in the chunks, checking if the [structured metadata][] key `traceID` with value `3c0e3dcd33e7` is present.

With accelerated filtering, Loki is able to skip most of the chunks and only process the ones where we have a statistical confidence that the structured metadata pair might be present.

## Adding data to blooms

To make data available for query acceleration, send [structured metadata][] to Loki. Loki builds blooms from all strucutred metadata keys and values.

## Querying blooms

Loki will check blooms for any [label filter expression][] that satisfies _all_ of the following criteria:

* The label filter expression using **string equality**, such as `| key="value"`.
* The label filter expression is querying for structured metadata and not a stream label.
* The label filter expression is placed before any [parser expression][], [labels format expression][], [drop labels expression][], or [keep labels expression][].

To take full advantage of blooms, ensure that filtering structured metadata is done before any parse expression:

```logql
{cluster="prod"} | logfmt | json | detected_level="error"  # NOT ACCELERATED: structured metadata filter is after a parse stage
{cluster="prod"} | detected_level="error" | logfmt | json  # ACCELERATED: structured metadata filter is before any parse stage
```

[structured metadata]: {{< relref "../get-started/labels/structured-metadata" >}}
[label filter expression]: {{< relref "../query/log_queries/_index.md#label-filter-expression" >}}
[parser expression]: {{< relref "../query/log_queries/_index.md#parser-expression" >}}
[labels format expression]: {{< relref "../query/log_queries/_index.md#labels-format-expression" >}}
[drop labels expression]: {{< relref "../query/log_queries/_index.md#drop-labels-expression" >}}
[keep labels expression]: {{< relref "../query/log_queries/_index.md#keep-labels-expression" >}}
