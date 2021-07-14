---
title: Developer Guide
---

# Developer Guide

This guide tries to ease making contributions to Loki.

<div class="mermaid">
sequenceDiagram
    participant l as Loki
    participant handler as transport.Handler

    participant r as queryrange.RoundTripper
    participant lr as limitedRoundTripper
    participant codec as queryrange.Codex 
    participant instant as instantMetrics
    participant sharding as queryrange.Middleware
    participant ast as astMapperware
    participant query as logql.Query

    Note right of r: Setup
    l->>+r: queryrange.NewTripperware
    r->>+instant: NewInstantMetricsTripperWare
    instant->>+sharding: NewQueryShardMiddleware
    sharding->>+ast: newASTMapperware
    ast-->>-sharding: astMapperware
    sharding-->>-instant: queryrange.Middleware
    instant->>+lr: NewLimitedRoundTripper
    lr-->>instant: limitedRoundTripper
    instant-->>r: limitedRoundTripper(queryMiddleware)
    deactivate instant
    deactivate lr
    deactivate r

    Note right of r: Query
    handler->>+r: RoundTrip
    r->>+lr: RoundTrip 
    lr->>+instant: Warp(...).Do()
    instant->>+ast: ShardedEngier.Query
    ast-->>-instant: logql.Query
    instant->>+query: Exec

    query->>+query: Eval
    query->>+query: evalSample
    query->>+DownstreamEvaluator: StepEvaluator
    DownstreamEvaluator->>+DownstreamEvaluator: Downstream
    
    DownstreamEvaluator-->>-query: StepEvaluator
    query->>+StepEvaluator: Next
    StepEvaluator-->>-query: promql.Vector
    query-->>-query: promql_parser.Value
    query-->>-query: logql.engine.query

    query-->>-instant: logqlmodel.Result
    instant-->>-lr: LokiResponse | LokiPromResponse
    lr->>+codec: EncodeResponse
    codec-->>-lr: http.Response
    lr-->>-r: http.Response
    r-->>-handler: http.Response
</div>
<script async src="https://unpkg.com/mermaid@8.2.3/dist/mermaid.min.js"></script>
