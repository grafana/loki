---
title: "0006: Expose Split Logic in API"
description: "0006: Expose Split Logic in API"
aliases: 
- ../../lids/0006-api-expose-split/
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

Loki has an internal logic to split log and metric queries by time into multiple queries. However, this logic is not
accessible outside of the code base. This proposal intends to create an API for clients to split queries by exposing the
internal split logic.

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

This proposal does not aim to provider pagination for query results.

## Proposals

### Proposal 0: Do nothing

Without an API each client will have to use a LogQL parser.

*Pros*
- The split logic in Loki can be changed at will without breaking client behavior.
- There is no maintanence overhead for an API.

*Cons*
- Currenlty, the LogQL grammar is specific to Go. It is not easy to port it and the parser to other languages.
- Any changes to the splitting logic must be implemented for each client/platform.

### Proposal 1: Expose Splitting in an API

A new endpoint `GET /loki/api/v1/split_query` is introduced that takes an `splits` parameter and the same parameters as the [/loki/api/v1/query_range](https://grafana.com/docs/loki/latest/reference/loki-http-api/#query-logs-within-a-range-of-time) endpoint.

The `splits` parameter defines the number of desired splits. The API is allowed to return fewer splits than requested.

The response body is JSON encoded

```json
{ 
  "resultType": "matrix" | "streams" | "vector",
  “links”: [
  {
    “rel”: “???”,
    “href”: “/loki/api/v1/query_range?start=10&end=200&limit=20&direction=forward?query=...”},
  {
    “rel”: “???”,
    “href”: “/loki/api/v1/query_range?start=30&end=200&limit=20&direction=forward?query=...”}
  ]
}
```

*Pros*
- Clients can split queries independent on the implemation language and platform.
- Split logic is controlled by Loki and not the client. This means it can be improved e.g. by introducing sharding
  labels.

*Cons*
- A new API endpoint increases the compatiblity surface area and thus maintanence overhead for Loki maintainers.

