---
title: Ensure query fairness within tenants using actors
menuTitle: Query fairness
description: Describes methods for guaranteeing query fairness across multiple actors within a single tenant using the scheduler.
weight:
---

# Ensure query fairness within tenants using actors

Loki uses [shuffle sharding](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/shuffle-sharding/)
to minimize impact across tenants in case of querier failures or misbehaving
neighboring tenants.

When there are potentially a lot of different actors using the same tenant to
query logs, such as users accessing Loki from Grafana or via LogCLI or other
applications using the HTTP API, it can lead to contention between queries of
different users, because they all share the same resources for a tenant.

In that case, as an operator, you would also want to ensure some sort of query
fairness across these actors within the tenants. An actor could be a Grafana user,
a CLI user, or an application accessing the API. To achieve that, Loki
introduced hierarchical scheduler queues in version 2.9 based on
[LID 0003: Query fairness across users within tenants](https://grafana.com/docs/loki/<LOKI_VERSION>/community/lids/0003-queryfairnessinscheduler/)
and they are enabled by default.

## What are hierarchical queues and how do they work

To understand hierarchical queues, we first need to know that in the scheduler
component each tenant has its own first in first out (FIFO) queue where
sub-queries are enqueued. Sub-queries are queries that result from splitting
and sharding of a query sent by a client using HTTP.

{{< admonition type="note" >}}
Hierarchical queues are managed by the query-scheduler, which is active by
default in all current deployment modes (monolithic, high-availability (HA)
monolithic, and microservices). If you have explicitly configured
`frontend_worker.frontend_address` to connect queriers directly to the query
frontend instead of using the scheduler, the `X-Loki-Actor-Path` header has
no effect, and all sub-queries go to the tenant's single queue.
{{< /admonition >}}

Tenant queues are the first level of the queue hierarchy. When a tenant
executes a query without any further controls, all of its sub-queries are
enqueued to the first level queue.

The second level of the queue hierarchy is that the tenant can have sub-queues.

Similar to how shuffle sharding assigns queries at the tenant level, each time
the Loki Scheduler makes a round-robin pick at the second level of the query
hierarchy, it selects a query from the tenant’s local queue and subqueues.

![Hierarchical queues](./hierarchical-queues.png)

The figure above shows that a tenant queue has a local queue, which is a leaf
node in the queue tree, and a set of sub-queues. Each sub-queue, again like the
tenant queue, consists of a local queue, and possible sub-queues, resulting in
a recursive tree structure.

So, how can we make use of these tree-like queue structures to achieve query fairness?

## How to control query fairness

As already mentioned, by default, sub-queries are only enqueued at the first
(tenant) level of the queue tree. The tenant is provided by the `X-Scope-OrgID`
header that is required when running Loki in multi-tenant mode.

You use the HTTP header `X-Loki-Actor-Path` to control to which sub-queue a
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
header to enqueue their queries to their own sub-queue.

Since the scheduler chooses the next task for a tenant in a round-robin manner,
if all of a tenant's queries use an actor-path header, both actors (in our
case human users) get their 50% share when the scheduler dequeues a sub-query
to send to the querier.

However, the tenant's local queue also takes part in the same round-robin
rotation as the sub-queues. If some of a tenant's queries don't include the
`X-Loki-Actor-Path` header, those queries wait in the tenant's local queue,
and that queue competes for its own share alongside the sub-queues. So with N
actor sub-queues and sub-queries also waiting in the tenant's local queue,
each of the N+1 queues gets 1/(N+1) of the share. In our example with two
users, if some sub-queries also land in the local queue, the local queue gets
1/3 and each sub-queue gets 1/3 of the total. A sub-queue drops out of the
rotation once it drains, so the remaining queues' shares increase as actors
go idle.

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
controlled by the Loki `-query-scheduler.max-queue-hierarchy-levels` CLI argument
or its respective YAML configuration block:

```yaml
query_scheduler:
  max_queue_hierarchy_levels: 2 # defaults to 3
```

It is advised to keep the levels at a reasonable level (ideally 1 to 3 levels),
both for performance reasons as well as for the understanding of how query
fairness is ensured across all sub-queues.

{{< admonition type="note" >}}
`max_queue_hierarchy_levels` counts only the `|`-separated segments in the
`X-Loki-Actor-Path` header value, not the tenant level. With the default of
`3`, a header value can have up to three segments, for example
`users|team|joe`.

If a request's `X-Loki-Actor-Path` has more segments than
`max_queue_hierarchy_levels` allows, Loki doesn't truncate it or fall back to
the tenant queue. The scheduler rejects the query, and Loki returns an HTTP
500 error whose message names both the header and the
`-query-scheduler.max-queue-hierarchy-levels` setting. Only that query fails;
other queries from the same client are not affected.

Setting `max_queue_hierarchy_levels` to `0` disables hierarchical queues.
The scheduler then ignores the `X-Loki-Actor-Path` header and enqueues all of
a tenant's sub-queries in the tenant's local queue.
{{< /admonition >}}

## Enforcing headers

In the examples above the client that invoked the query directly against Loki also provided the
HTTP header that controls where in the queue tree the sub-queries are enqueued. However, as an operator,
you would usually want to avoid this scenario and control yourself where the header is set.

When using Grafana as the Loki user interface, you can, for example, create multiple data sources
with the same tenant, but with a different additional HTTP header
`X-Loki-Actor-Path` and restrict which Grafana user can use which data source.
For instructions on adding a custom HTTP header to a data source, refer to
[Configure the Loki data source](https://grafana.com/docs/grafana/<GRAFANA_VERSION>/datasources/loki/configure/#custom-http-headers).

Alternatively, if you have a proxy for authentication in front of Loki, you can
pass the (hashed) user from the authentication as downstream header to Loki.
