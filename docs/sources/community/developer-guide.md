---
title: Developer Guide
---

# Developer Guide

This guide tries to ease making contributions to Loki.

## Frontend Sharding

The following sequence diagram shows the calls during an instant metrics query call.

![Instant Query Diagram](sequence-instant-query.mmd.svg)

At first the frontend is setup up with all round trip and query middleware (numbers 1-9).

When a request comes in (10) the round trip middlewares call `RoundTrip` on the next nested middleware (10 and 11). The `limitedRoundTripper` translates the request to call out the `Do` method of nested `queryrange.Middleware` (12). The middleware then creates a query (13 and 14) and executes it (15). The execution triggers the downstream (18 and 19) which then runs the query shards in parallel (20-23). The results are accumulated (22) and bubbled all the way up to the original `RoundTrip` call (24-33).
