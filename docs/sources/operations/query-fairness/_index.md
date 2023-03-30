---
title: Query fairness within tenants
menuTitle: Query fairness
description: The scheduler can guarantee query fairness across multiple actors within a single tenant.
weight: 101
---

# Query fairness within tenants

Loki uses [shuffle sharding]({{<relref "../shuffle-sharding/_index.md">}})
to minimize impact across tenants in case of querier failures or misbehaving
neighbouring tenants.

When there are potentially a lot of different users using the same tenant to
query logs, such as users accessing Loki from Grafana or via LogCLI or other
applications using the HTTP API, it can lead to contention between queries of
different users, because they all share the same tenant.

In that case, as an operator, you would also want to ensure some sort of query
fairness across these actors within the tenants. To achieve that, Loki
introduced hierarchical scheduler queues in version 2.9 based on
[LID 0003: Query fairness across users within tenants]({{<relref "../../lids/0003-QueryFairnessInScheduler.md">}})
and they are enabled by default.

## What are hierarchical queues and how do they work

To understand hierarchical queues, we first need to know that in the scheduler
component each tenant has it's own first in first out (FIFO) queue where
sub-queries are enqueued. Sub-queries are queries that result from splitting
and sharding of a query sent by a client using HTTP.
Depending on whether shuffle sharding is turned off or on, either all queriers
or a subset of those queriers are allowed to dequeue from a tenant queue.

Tenant queues are the first level of the queue hierarchy. When a tenant
executes a query without any further controls, all of its sub-queries are
enqueued to the first level queue.

However, tenant queues can also have sub-queues.
When a querier dequeues the next item from a tenant queue for it to be
processed, the algorithm returns a sub-query round robin across the first level
(local queue) and all it's sub-queues.

![Hierarchical queues](./hierarchical-queues.png)

The figure above shows that a tenant queue has a local queue, which is a leaf
node in the queue tree, and a set of sub-queues. Each sub-queue, again like the
tenant queue, consists of a local queue, and possible sub-queues, resulting in
a recursive tree structure.

So, how can we make use of these tree-like queue structure to achieve query fairness?

## How to control query fairness

As already mentioned, by default, sub-queries are only enqueued at the first
(tenant) level of the queue tree. The tenant is provided by the `X-Scope-OrgID`
header that is required when running Loki in multi-tenant mode.

The HTTP header `X-Loki-Actor-Path` can be used to control to which sub-queue a
query (or more correctly its sub-queries) is enqueued.

The following example shows a `curl` command that invokes the HTTP endpoint for range queries
and passes both the `X-Scope-OrgID` and the `X-Loki-Actor-Path` headers.

```bash
curl -s http://localhost:3100/loki/api/v1/query_range?xxx \
    -H 'X-Scope-OrgID: grafana' \
    -H 'X-Loki-Actor-Path: joe'
```

The query that this request invokes ends up in the sub-queue `joe` of the
tenant queue `grafana`. Another user can use their own name in the actor path
header to enqueue their queries in their own sub-queue.

Since the tenant queue chooses the next task in a round-robin manner, both
actors (in our case human users) get their 50% share when a querier dequeues
from that tenant.

Even when there are tasks in the local queue of the tenant, the local queue get
1/3 and each sub-queue gets 1/3 of their share.

With N actors, each actor get 1/Nth of their share.

As the explained implementation and the header name already suggest, it is
possible to enqueue queries several levels deep. To do so, you can construct a
path to the sub-queue using the `|` delimiter in the header value, as shown in
the following examples.

```bash
curl -s http://localhost:3100/loki/api/v1/query_range?xxx \
    -H 'X-Scope-OrgID: grafana' \
    -H 'X-Loki-Actor-Path: users|joe'

curl -s http://localhost:3100/loki/api/v1/query_range?xxx \
    -H 'X-Scope-OrgID: grafana' \
    -H 'X-Loki-Actor-Path: apps|logcli'
```

There is a limit to how deep a path and thus the queue tree can be. This is
controlled by Loki's `-query-scheduler.max-queue-hierarchy-levels` CLI argument
or its respective YAML configuration block:

```yaml
query_scheduler:
  max_queue_hierarchy_levels: 2
```

It is advised to keep the levels at a reasonable level (ideally 1 to 3 levels),
both for performance reasons as well as for the understanding of how query
fairness is ensured across all sub-queues.

## Enforcing headers

In the examples above the client that invoked the query directly against Loki also provided the
HTTP header that controls where in the queue tree the tasks are enqueued. However, as an operator,
you would usually want to avoid this scenario and control yourself where the header is set.

When using Grafana as the Loki user interface, you can, for example, create multiple datasources
with the same tenant, but with a different additional HTTP header
`X-Loki-Scope-Actor` and restrict which Grafana user can use which datasource.

Alternatively, if you have a proxy for authentication in front of Loki, you can
pass the (hashed) user from the authentication as downstream header to Loki.
