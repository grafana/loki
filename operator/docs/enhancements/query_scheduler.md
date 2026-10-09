---
title: query-scheduler-v2-frontend
authors:
  - "@btaani"
reviewers:
  - "@joaobravecoding @xperimental @cahartma"
creation-date: 2026-09-29
last-updated: 2026-09-29
tracking-link:
  - https://redhat.atlassian.net/browse/LOG-9664
see-also: []
replaces: []
superseded-by: []
---

# Query Scheduler: V2 Frontend Architecture

## Summary

This enhancement introduces the `query-scheduler` component to LokiStack deployments. The query-scheduler acts as a dedicated work-queue between the query-frontend and the queriers, enabling the V2 frontend architecture where all scheduling communication happens over gRPC/Protobuf end-to-end.

## Motivation

### V1 frontend architecture and the operator today

In Loki's V1 frontend architecture, the queriers are directly connected to query-frontends via `frontend_worker.frontend_address`. The query-frontend holds an in-memory queue of pending sub-queries and dispatches them to whichever querier worker pulls next. This design is simple but couples queue management tightly to the query-frontend process.

The operator currently deploys this V1 architecture, it configures`frontend_worker.frontend_address` to point at the query-frontend gRPC service, and all queriers connect to it directly.

### What has changed upstream

Loki 3.7 changed the default value of `frontend.encoding` from `json` to `protobuf` ([PR](https://github.com/grafana/loki/pull/23985)). Protobuf encoding is only supported on the V2 frontend path, which means `frontend.encoding` is a field that exists only on the V2 frontend struct. V1 ignored the setting entirely. As a result, this default change has no effect on the operator today: managed LokiStacks remain on V1 and the new default is silently ignored.

However, the default change is an upstream signal that JSON encoding is on a deprecation path. Once JSON encoding is removed entirely, V1 becomes unviable and the operator must have switched to V2 or all managed query paths break.

Introducing the query-scheduler is proactive forward-compatibility. It positions the operator ahead of the JSON removal deadline rather than reacting to it. It also immediately unlocks V2-exclusive capabilities once deployed: protobuf end-to-end on the scheduling path, and newer LogQL functions such as `approx_topk` that require protobuf internally.

### Goals

- Deploy a `query-scheduler` component as part of every LokiStack.
- Rewire the query path so that query-frontends and queriers both connect to the scheduler (V2 architecture), enabling `frontend.encoding=protobuf`.
- Establish baseline resource profiles for the query-scheduler across all supported LokiStack sizes (this is an open question; see below).
- Support TLS-encrypted gRPC for the query-scheduler when `GRPCEncryption` is enabled.

### Non-Goals

- Providing a migration path from V1 to V2 for existing LokiStack instances (this can be addressed in a follow-up enhancement once resource profiles are established).
- Horizontal pod autoscaling for the query-scheduler.
- Separate ruler query pool: Loki docs recommend a dedicated query-frontend and querier pool for ruler queries so that rule evaluation is not starved by ad-hoc query traffic. The operator currently shares a single pool for both. Introducing a separate ruler pool is a distinct enhancement.

## Proposal

### Architecture Change

**V1 (current)**

```
Client → Gateway → QueryFrontend
                       ↑ Pull queries from QueryFrontend(gRPC, frontend_worker.frontend_address)
                    Querier
```

**V2 (new)**

```
Client → Gateway → QueryFrontend → QueryScheduler
                                        ↑ Pulls queries from QueryScheduler (gRPC, frontend_worker.scheduler_address)
                                     Querier
```
As per the Loki documentation, the read path from the query-frontend to the queriers goes as follows:
1. The query frontend receives an HTTP GET request with a LogQL query.
2. The query frontend splits the query into sub-queries and passes them to the query scheduler.
3. Query scheduler enqueues them in an internal in-memory queue (there is a queue for each tenant to guarantee the query fairness across all tenants).
4. The queriers that connect to the query scheduler act as workers that pull their jobs from the queue, execute them, and return them to the query frontend for aggregation.

The querier then executes the queries, fetches the logs, and sends them back directly to the query-frontend, which then forwards the results back to the client.

#### Benefits of using a query-scheduler
- Efficient multi-frontend scaling: in V1, each frontend holds its own independent queue. Querier workers are statically connected to a specific frontend pod, so capacity is not shared across frontends. The scheduler centralises the queue so all querier workers pull from a single global pool, eliminating idle capacity when one frontend is overloaded and another is not.
- Tenant fairness: the scheduler maintains a separate queue per tenant and uses round-robin dispatch across them, preventing any single tenant from monopolising querier capacity. This fairness is enforced globally across all frontend instances, not per-frontend as in V1.
- Query class isolation: the scheduler separates queries by type (e.g. ingester-bound vs store-gateway-bound) using hierarchical queues. Degradation in one backend (e.g. slow store-gateways) cannot starve queries targeting a healthy backend (e.g. ingesters), because they have independent queue budgets.
- Protobuf end-to-end: the scheduler is required to activate the V2 frontend path, which is the only path that supports frontend.encoding=protobuf. This unblocks newer LogQL functions and is a prerequisite for when Grafana eventually removes JSON encoding support.


### API Extensions

#### `LokiTemplateSpec`

A new optional field is added:

```go
// QueryScheduler defines the query-scheduler component spec.
//
// +optional
// +kubebuilder:validation:Optional
// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Query Scheduler pods"
QueryScheduler *LokiComponentSpec `json:"queryScheduler,omitempty"`
```

#### `LokiStackComponentStatus`

A new status field tracks per-pod state of the query-scheduler:

```go
// QueryScheduler is a map of per-pod status for the query-scheduler deployment.
QueryScheduler PodStatusMap `json:"queryScheduler,omitempty"`
```

### Kubernetes Resources Created

For each LokiStack the operator will additionally create:

| Resource | Name | Description |
|---|---|---|
| `Deployment` | `<stack>-query-scheduler` | Query-scheduler pods |
| `Service` (headless, gRPC) | `<stack>-query-scheduler-grpc` | Scheduler gRPC endpoint |
| `Service` (HTTP) | `<stack>-query-scheduler-http` | Metrics / health endpoint |
| `PodDisruptionBudget` | `<stack>-query-scheduler` | Max 1 unavailable |
| `ServiceMonitor` (optional) | `<stack>-query-scheduler-monitor` | Prometheus scraping when `serviceMonitors` gate is enabled |

### Loki Configuration Changes

#### Discovery mode: static address (chosen approach)

Loki supports two ways for frontends and queriers to find schedulers:

1. **Static address**: set `frontend.scheduler_address` and `frontend_worker.scheduler_address`
   to a fixed service FQDN.
2. **Ring-based discovery** (default): set `query_scheduler.use_scheduler_ring: true`; schedulers
   advertise themselves via the memberlist gossip ring and are discovered dynamically.

The operator uses static address to stay consistent with existing patterns, every other inter-component address in the operator (compactor_grpc_address, tail_proxy_url, index_gateway_client.server_address) uses a static service FQDN that the operator constructs itself.

And the `frontend_worker` and `frontend` blocks should read:
```yaml
frontend_worker:
  scheduler_address: <query-scheduler-grpc-svc>:9095

frontend:
  scheduler_address: <query-scheduler-grpc-svc>:9095
```

**New `query_scheduler` block**

```yaml
query_scheduler:
  max_outstanding_requests_per_tenant: 32000
```
Note: 32000 is Loki's upstream default.

### Implementation Details

#### Deployment

The query-scheduler is stateless and can be deployed as a `Deployment`.

#### Replication Factor
Loki's current implementation sets the RF of the query-scheduler component to 2. The same applies to the operator except for `1x.demo` where RF is 1.


#### Observability

The query-scheduler exposes the following Prometheus metrics:

- `loki_query_scheduler_inflight_requests` The only scheduler metric with [official documentation](https://grafana.com/docs/loki/latest/operations/autoscaling_queriers/); recommended signal for querier autoscaling.
- `loki_query_scheduler_queue_duration_seconds`
- `loki_query_scheduler_connected_querier_clients`
- `loki_query_scheduler_connected_frontend_clients`
- `loki_query_scheduler_running`
- `loki_scheduler_queue_length`
- `loki_scheduler_discarded_requests_total`
- `loki_scheduler_enqueue_count`

For clusters still on V1 (no scheduler deployed), the equivalent metrics for estimating future scheduler load are `loki_query_frontend_queue_length`, `loki_query_frontend_queue_duration_seconds`, and `loki_query_frontend_connected_clients`.

### Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Migration disruption for existing V1 LokiStacks | Addressed in a follow-up; this enhancement targets new installations |

## Design Details

### Open Questions

1. Resource requirements for query-scheduler: this is the primary open question this spike is meant to answer. The query-scheduler holds an in-memory queue of pending sub-queries per tenant. Memory usage scales with `max_outstanding_requests_per_tenant × active_tenants`. CPU usage is expected to be low (mostly queue bookkeeping and gRPC plumbing).

   **Investigation plan**: run the PoC branch against a realistic load profile (matching each supported t-shirt size) and record peak CPU and memory consumption of the scheduler pod. Use those measurements to set `Requests` values and derive sensible `Limits`.
2. `max_outstanding_requests_per_tenant` default: Loki's upstream default is 32,000. The operator uses this default unchanged. The right value for a given deployment depends on tenant count and query concurrency; it could be surfaced as a `LokiStack` spec field once resource profiles are established.
3. Does a separate ruler pool require a separate query-scheduler, or is one scheduler with prioritisation sufficient? With the current proposed design, all queries (ad-hoc or ruler) share the same scheduler queue. The scheduler's round-robin prevents contention between tenant queries but it doesn't differentiate between query classes within the same tenant. There are two possible solutions to isolate based on class:
    - A second scheduler dedicated to the ruler pool.
    - Priority queuing within the scheduler so ruler queries are dispatched ahead of non-ruler queries. Loki's scheduler already has a hierarchical queue (`max_queue_hierarchy_levels`, default 3) that places different query classes at different levels and round-robins across them, this is the Loki-native path toward per-class prioritisation without needing a second scheduler.

## Drawbacks
- The PoC removes support for V1-style `frontend_address`; existing clusters would need to drain in-flight queries before the scheduler is ready. A proper migration strategy is deferred.
- `tls_insecure_skip_verify: true` on querier outbound connections: when `GRPCEncryption` is enabled, the querier's `frontend_worker.grpc_client_config` is shared between two outbound connections, querier→scheduler (to pull jobs) and querier→frontend (to deliver results). These connections need different TLS server names, but only one config exists. Since pod IPs have no cert SANs and the result-delivery connection dials a pod IP, server cert verification is disabled on both. Server-side mTLS is still enforced (servers verify the querier's client cert), so unauthenticated clients cannot connect. Loki's own reference deployment does not enable TLS on these internal connections at all and relies on network-level isolation instead. The proper fix? per-pod certificates, which requires changes to the built-in cert manager and is deferred to a follow-up.

