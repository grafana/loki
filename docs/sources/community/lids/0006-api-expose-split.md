---
title: "0006: Expose Split Logic in API"
description: "0006: Expose Split Logic in API"
---

# 0006: Expose Split Logic in API

**Author:** Karsten Jeschkies (karsten.jeschkies@grafana.com)

**Date:** 03/2025

**Sponsor(s):** @trevorwhitney

**Type:** API

**Status:** Review

**Related issues/PRs:** N/A

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):** N/A

---

## Background

Loki has an internal logic to split and shard log and metric queries by time into multiple queries. However, this logic is not
accessible outside of the code base. This proposal intends to create an API for clients to split queries by exposing the
internal split logic.

A split query is devided by time. The results of a split query can be concatenated in order to form the final
result.

A sharded query is devided by label values. The results of a sharded cannot be simply concatenated but require some
extra logic to form the final result. Some queries, such as `topk` cannot be sharded at all.

## Problem Statement

Loki clients such as the Grafana Loki datasource or the [Trino Loki
connector](https://github.com/trinodb/trino/tree/master/plugin/trino-loki) benefit from splitting LogQL queries into multiple sub-queries either to process
smaller chunks or to distribute work on query results.

Splitting a query requires parsing the LogQL query first but there are no parsers for other languages except Go and
JavaScript.

## Goals

The intended goal is to enable any client to split a query into multiple sub-queries that can be either executed
sequentially or in parallel. The joined result of the sub-queries must be the same as executing the same query.

## Non-Goals

This proposal does not aim to provide pagination for query results.

## Proposals

### Proposal 0: Do nothing

Without an API each client will have to use a LogQL parser.

*Pros*
- The split logic in Loki can be changed at will without breaking client behavior.
- There is no maintanence overhead for an API.

*Cons*
- Currently, the LogQL grammar is specific to Go. It is not easy to port it and the parser to other languages.
- Any changes to the splitting logic must be implemented for each client/platform.

### Proposal 1: Expose Splitting in an API

A new endpoint `GET /loki/api/v1/split_query` is introduced that takes a `splits` parameter and the same parameters as the [/loki/api/v1/query_range](https://grafana.com/docs/loki/latest/reference/loki-http-api/#query-logs-within-a-range-of-time) endpoint. The new endoint returns sub-queries split by time.

The `splits` parameter optionally defines the number of desired splits. The API is allowed to return fewer splits than requested.

The `limit` parameter has extended semantics. Setting it to `0` for a log stream query indicates to query all logs.

The response body is JSON encoded:

```json
{ 
  "resultType": "matrix" | "streams" | "vector",
  "subqueries": [
    {
      start: <timestamp nanoseconds>,
      end: <timestamp nanoseconds>,
      limit: <number>,
      query: <query string> 
    },
    {
      start: <timestamp nanoseconds>,
      end: <timestamp nanoseconds>,
      limit: <number>,
      query: <query string> 
    }
  ]
}
```

*Pros*
- Clients can split queries independent on the implemation language and platform.
- Split logic is controlled by Loki and not the client. This means it can be improved, for example, by introducing sharding
  labels.

*Cons*
- A new API endpoint increases the compatiblity surface area and thus maintanence overhead for Loki maintainers.

### Proposal 2: Support Apache Arrow Flight RPC

Loki could support Apache [Arrow Flight RPC](https://arrow.apache.org/docs/format/Flight.html) which is designed to
exchange large data sets in shards between services.

*Pros*
- Supporting an open standard comes with support for other non-Loki clients.

*Cons*
- Loki would have to support Apache Arrow which make the implementation more complicated.
- Arrow Flight RPC assumes the data is being queried on the first request. Which means all shards are available at the
  same time. However, the intent of this document is that shards can be queried independently.
