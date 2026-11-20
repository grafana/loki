---
title: Logline (Experimental)
menuTitle: Logline
description: Learn how Logline accelerates needle in a haystack log searches in Grafana Loki, how its components work, and which queries can benefit.
weight:
keywords:
  - logline
  - query acceleration
  - n-gram index
---

# Logline (Experimental)

{{< admonition type="warning" >}}
Logline is an [experimental feature](https://grafana.com/docs/release-life-cycle/). Configuration options, index formats, and behavior can change. Validate index coverage and query results before enabling live acceleration.
{{< /admonition >}}

Logline accelerates "needle in a haystack" log searches in Grafana Loki: queries that search large volumes of logs for a small number of matches, such as a specific trace ID, request ID, or error message. It uses an additional index to identify time ranges that might contain matching logs. Loki can skip time ranges that the index rules out, reducing the amount of log data it fetches and scans.

For example, consider a query for a trace ID across a cluster:

```logql
{cluster="prod"} |= "3c0e3dcd33e7"
```

Without Logline, Loki finds log data matching `{cluster="prod"}`, then scans it for the trace ID. With Logline, Loki first looks up the search text in the Logline index and uses the resulting candidate time ranges to narrow that work. The normal LogQL pipeline still evaluates logs within those ranges.

Logline is an optional secondary index: an additional lookup structure that helps Loki decide where to search. It does not replace log storage or introduce a new query language. You continue to use LogQL, while Loki uses the index behind the scenes for eligible queries.

Logline is most useful for searches for uncommon text, such as trace IDs, request IDs, and error messages, across large amounts of data. Common search text, short search values, and queries that match much of the selected data might not benefit enough to offset the index lookup cost.

## How Logline works

Logline indexes overlapping sequences of bytes, called *n-grams*, extracted from raw log lines, structured metadata values, and label values. An index maps these terms to documents representing time buckets. With the default `v4` format, each document covers 16 seconds of log time, and indexes are shared across tenants.

For example, if a trace ID appears only during a short period of a day, the index can point Loki toward that period instead of having it scan the entire day.

At query time, Logline identifies fixed text that a log entry must contain to match the query, called a *mandatory literal*. It looks up that text in the index and combines the results into candidate time ranges, called *hints*. These are candidates, not exact matches: indexed text in one time bucket can come from different log entries, fields, or tenants. Normal LogQL evaluation removes those extra candidates.

Building indexes and querying them happen independently. Builders continuously process incoming Kafka records, while query components discover the uploaded indexes and use them for searches. New data is not searchable through the index until building, uploading, and discovery have completed; recent data continues through the normal query path.

{{< mermaid >}}
flowchart TD
    D[Distributor] --> K[Kafka log topic]
    K --> B[Logline index builders]
    B --> O[Logline indexes in object storage]
    S[Logline scheduler] --> C[Logline compactors]
    C <--> O
    S -->|Plans compaction and retention| O
    Q[LogQL query] --> F[Query frontend]
    O -->|Candidate time ranges| F
    F --> R[Queriers]
    R --> L[Log storage and ingesters]
{{< /mermaid >}}

Unlike [Bloom filters](../bloom-filters/), Logline builds its index from Kafka records rather than scanning stored chunks. It narrows candidate time ranges rather than testing structured metadata pairs against Bloom filters. It does not require a Bloom Planner or Bloom Gateway.

## Components

Logline adds three services to your deployment: index builders, a compaction scheduler, and compactors. It also integrates with Loki's existing query frontends and queriers. Object storage connects the indexing and query paths; there is no separate Logline query gateway.

### Index builders

The `logline-index-builder` service reads log records from Kafka and builds the additional search index. It extracts searchable text from log lines, structured metadata, and label values, then writes index files to object storage.

Builders work continuously as records arrive. They do not read historical chunks to build indexes, so enabling Logline does not automatically index logs already in object storage. Multiple builders share Kafka partitions through their own consumer group, allowing indexing capacity to scale independently of query capacity.

### Compaction scheduler

The `logline-scheduler` service manages index maintenance. As builders upload files, the scheduler identifies compatible files that can be merged and assigns that work to compactors. It also deletes expired indexes and old files that have been replaced by compaction.

This is a Logline-specific compaction scheduler, not Loki's query scheduler. It does not assign Kafka records to builders or execute user queries. Run only one active instance per index store and preserve its job state across restarts.

### Compactors

The `logline-compactor` service requests jobs from the scheduler, reads the source index files, merges them, and uploads the resulting index. Merging many small files into fewer, larger files reduces the number of objects that query components need to discover and read.

Compactors operate on Logline indexes, not the underlying log data. Add compactor workers when index maintenance cannot keep up with the files builders produce.

### Index storage

Logline stores index files and their metadata in object storage, under a dedicated prefix. This is the shared source of index data for builders, compactors, and query components.

Query components periodically discover new files rather than receiving each builder upload directly. That discovery delay is one reason indexes become usable after the logs themselves have been ingested. All components must use the same bucket and prefix.

### Query frontends

The query frontend receives a LogQL request and decides whether Logline can help. For an eligible query, it starts an index lookup and uses the resulting hints while dividing the query into smaller time-based requests, called *splits*.

A split with no candidate time ranges can be skipped. Otherwise, the frontend passes the relevant hints to a querier. Queries that are ineligible, too small, or inside the protected recent-data window use the normal query path.

### Queriers

Queriers fetch and evaluate the actual log data. With Logline hints, they can avoid unrelated chunks and blocks within chunks, reducing the amount of data downloaded and scanned.

The index does not return the final log results. Queriers still apply the full LogQL query to the candidate data, including filters that Logline cannot accelerate.

## Query examples

Logline needs searchable text that a matching log entry must contain. The examples below distinguish queries that can use the index from queries that cannot currently benefit from it.

### Queries Logline can accelerate

These examples assume live mode is enabled, index coverage is complete for the queried period, and the query meets the configured minimum size. Being eligible does not guarantee a speedup: the search text must also be selective enough to rule out useful amounts of data.

| LogQL query | Why Logline can help |
| --- | --- |
| `{app="api"} \|= "3c0e3dcd33e7"` | Every matching line must contain the trace ID, so Logline can find candidate time ranges for that text. |
| `{app="api"} \|= "1234567890123456789"` | The long numeric ID provides searchable text that Logline can use to narrow the time ranges. |
| `{app="api"} \| trace_id="3c0e3dcd33e7"` | The structured metadata filter requires a specific value that Logline indexes. |
| `{app="api"} \| json \| trace_id="3c0e3dcd33e7"` | The parsed-field filter requires a value present in the original JSON log line. |
| `{app="api"} \|~ ".*database.*timeout.*"` | Every regex match must contain both `database` and `timeout`. Logline can use that required text to narrow the search. |
| `{app="api"} \|= "database timeout" != "healthcheck"` | The positive filter provides searchable text. The negative filter still runs normally on the candidate logs. |

For parsed fields, Logline looks up the value rather than an exact field-name/value pair. For regex filters, it uses required fixed text, not the full regular expression. The normal LogQL pipeline still checks the field names and complete filter conditions.

### Queries Logline does not currently accelerate

These examples still run as normal LogQL queries; they just do not receive live index acceleration.

| LogQL query | Why it does not use live acceleration |
| --- | --- |
| `{app="api"}` | There is no text filter for Logline to look up. Loki's normal label index handles the selector. |
| `{app="api"} != "healthcheck"` | A negative-only filter does not identify text that matching logs must contain. |
| `{app="api"} \|= "err"` | The search text is too short to provide usable index terms. |
| `{app="api"} \|= "123456"` | This numeric value is too short to provide usable index terms with the default index format. |
| `{app="api"} \|~ "[0-9]+"` | The regex matches many possible values without requiring any particular fixed text. |
| `{app="api"} \|= "database timeout" or "connection refused"` | Neither alternative is required in every match. Support for accelerating OR branches is a work in progress. |
| `{app="api"} \| json \| line_format "{{.message}}" \|= "database timeout"` | The text filter runs after `line_format` rewrites the line, so Logline cannot use it to search the original indexed text. |
| `count_over_time({app="api"} \|= "database timeout" [5m])` | This is a metric query. Live acceleration for metric queries is a work in progress; some metric queries can participate in dry-run verification. |

A query can still be eligible when it combines a usable positive filter with an unsupported filter. Numeric comparisons and pattern-match filters such as `|>` do not themselves provide searchable text, but they do not prevent another eligible filter from contributing it.

Pipeline order also matters beyond the examples above. Label rewriting, `unpack`, and parsing a rewritten line restrict which later field filters are eligible. Parsed values containing escaped quotes, backslashes, or control characters might not be eligible because their parsed representation differs from the indexed text.

## Next steps

- [Configure Logline]({{< relref "./configure.md" >}}) for prerequisites, deployment, tuning, retention, and monitoring.
- [Troubleshoot Logline]({{< relref "./troubleshooting.md" >}}) for high consumption lag, missing results, and queries that do not accelerate.
