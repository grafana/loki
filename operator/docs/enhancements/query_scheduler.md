---
title: query-scheduler-v2-frontend
state: discussion  # One of: draft, discussion, published, committed, abandoned
authors:
  - "@btaani"
reviewers:
  - "@joaobravecoding @xperimental @cahartma"
creation-date: 2026-09-29
last-updated: 2026-10-28
tracking-link: https://redhat.atlassian.net/browse/LOG-9664
see-also: []
replaces: []
superseded-by: []
proof-of-concept: https://github.com/grafana/loki/compare/main...btaani:query-scheduler-poc-code?expand=1
benchmark-results: 
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

- Separate ruler query pool: Loki docs recommend a dedicated query-frontend and querier pool for ruler queries so that rule evaluation is not starved by ad-hoc query traffic. The operator currently sets `ruler.evaluation.mode: local`, which means that the ruler doesn't depend on the query path.

## Proposal

### Architecture Change

**V1 (current)**

```
Client → Gateway → QueryFrontend
                       ↑ Pull queries (JSON over gRPC)
                    Querier
```

**V2 (new)**

```
Client → Gateway → QueryFrontend → QueryScheduler
                                        ↑ Pulls queries from QueryScheduler (Protobuf over gRPC)
                        ↑ Return results directly (Protobuf over gRPC)
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
2. **Ring-based discovery**: set `query_scheduler.use_scheduler_ring: true`; schedulers
   advertise themselves via the memberlist gossip ring and are discovered dynamically.

The goal is for the operator to continue using static address to stay consistent with existing patterns.

And the `frontend_worker` and `frontend` blocks should read:
```yaml
frontend_worker:
  scheduler_address: <query-scheduler-grpc-svc>:9095
  query_scheduler_grpc_client:
    tls_enabled: true
    tls_server_name: <query-scheduler-grpc-svc-fqdn>
    ...
  query_frontend_grpc_client:
    tls_enabled: true
    tls_server_name: <query-frontend-grpc-svc-fqdn>
    ...

frontend:
  grpc_client_config:
    tls_enabled: true
    tls_server_name: <query-scheduler-grpc-svc-fqdn>
    ...
```

**New `query_scheduler` block**

```yaml
query_scheduler:
  max_outstanding_requests_per_tenant: 32000
```
Note: 32000 is Loki's upstream default. See open question #2 below.

#### V1 fields ignored in V2

The following `frontend:` and `frontend_worker:` fields are V1-only. They are still parsed by Loki when V2 is active but their values are never used:

- `frontend.max_outstanding_per_tenant`: V1 queue depth limit, replaced by `query_scheduler.max_outstanding_requests_per_tenant`.
- `frontend_worker.frontend_address`: V1 direct querier→frontend address, replaced by `frontend_worker.scheduler_address`.

### Implementation Details

#### Deployment

The query-scheduler is stateless and can be deployed as a `Deployment`.

#### Replicas

To achieve high availability on the query path we will want to target running the query-scheduler component with 2 replicas.

#### Query Flow

When a query arrives at a query-frontend, the frontend splits it into sub-queries and sends them to one of the schedulers via a persistent gRPC stream. The scheduler holds the sub-queries in its per-tenant in-memory queue. Querier workers, which are goroutines running inside each querier pod, maintain persistent connections to the schedulers and continuously pull sub-queries from the queue. Once a querier executes a sub-query, it delivers the result directly back to the originating query-frontend (not through the scheduler), which then aggregates all results and responds to the client.

If a scheduler dies, all sub-queries sitting in its in-memory queue are lost, they are not persisted or replicated to the other scheduler. Frontends and queriers detect the broken connection and reconnect to the surviving scheduler, but any in-flight queries that had sub-queries in the dead scheduler's queue will timeout and the client must retry. Sub-queries that had already been picked up by a querier and were actively executing can still complete, since their results are returned directly to the frontend and do not pass through the scheduler on the way back. With two scheduler replicas, roughly half of in-flight sub-queries are affected when one dies.

#### Observability

The query-scheduler exposes the following Prometheus metrics:

- `loki_query_scheduler_inflight_requests` The only scheduler metric with [official documentation](https://grafana.com/docs/loki/latest/operations/autoscaling_queriers/); recommended signal for querier autoscaling.
- `loki_query_scheduler_queue_duration_seconds`
- `loki_query_scheduler_connected_querier_clients`
- `loki_query_scheduler_connected_frontend_clients`
- `loki_query_scheduler_running`
- `loki_query_scheduler_queue_length`
- `loki_query_scheduler_discarded_requests_total`
- `loki_query_scheduler_enqueue_count`

## Design Details

### Open Questions

1. Resource requirements for query-scheduler: this is the primary open question this spike is meant to answer. The query-scheduler holds an in-memory queue of pending sub-queries per tenant. Memory usage scales with `max_outstanding_requests_per_tenant × active_tenants`. CPU usage is expected to be low (mostly queue bookkeeping and gRPC plumbing).

   **Investigation plan**: run the PoC branch against a realistic load profile (matching each supported t-shirt size) and record peak CPU and memory consumption of the scheduler pod. Use those measurements to set `Requests` values and derive sensible `Limits`.
2. `max_outstanding_requests_per_tenant` default: in V1, the operator sets `frontend.max_outstanding_per_tenant: 4096`. In V2, that field belongs to the V1 frontend struct and is silently ignored. The effective queue limit becomes `query_scheduler.max_outstanding_requests_per_tenant`, which defaults to 32,000 upstream, which is an 8x increase. A higher limit means the queue can grow much deeper before clients are rejected. This will cause far fewer rejections, meaning the queue can grow very deep before clients are told to back off. A deeply filled queue will cause longer query-response delays instead of rejections, and potential memory spikes in the scheduler.
3. V1→V2 rollout impact: a rolling reconciliation is expected to be sufficient, but the upgrade path was not validated in this PoC. During the rollout, pending queries on pods being terminated will fail and require client-side retries.

