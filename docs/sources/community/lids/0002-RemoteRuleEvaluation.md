---
title: "0002: Remote Rule Evaluation"
description: "Remote Rule Evaluation"
aliases: 
- ../../lids/0002-remoteruleevaluation/
---

# 0002: Remote Rule Evaluation

**Author:** Danny Kopping (danny.kopping@grafana.com)

**Date:** 01/2023

**Sponsor(s):** @dannykopping

**Type:** Feature

**Status:** Accepted

**Related issues/PRs:** https://github.com/grafana/mimir/pull/1536

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):** N/A

---

## Background

The `ruler` is a component that evaluates alerting and recording rules. Loki reuses Prometheus' rule evaluation engine. The `ruler` currently operates by initialising a `querier` internally and evaluating all rules "locally" (i.e. it does not rely on any other components). Each rule group executes concurrently, and rules within the rule group are evaluated sequentially (this is an implementation detail from Prometheus).

Recording rules produce metric series which are sent to a Prometheus-compatible source. Alerting rules send notifications to Alertmanager when a condition is met. Both of these rule types can play a vital role in an organisation's observability strategy, and so their reliable evaluation is essential.

## Problem Statement

Rule evaluations can contain expensive queries. The `ruler` initialises a `querier`, but the `querier` does not have the capability to accelerate queries; the `query-frontend` component is responsible for query acceleration through splitting, sharding, caching, and other techniques.

An expensive rule query can cause an entire `ruler` instance to use excessive resources and even crash. This is highly problematic for the following reasons:

- slow rule evaluations can lead to subsequent rules in a group to be delayed or missed, leading to missing alerts or gaps in recording rule metrics
- excessive resource usage can impede the evaluation of rules for other tenants (noisy neighbour)

## Goals

- faster, more efficient rule evaluation
- greater isolation between tenants
- more reliable service

## Non-Goals

This proposal does not aim to make this option the default mode of evaluation; it should be optional because it increases operational complexity.

## Proposals

### Proposal 0: Do nothing

Loki's current `ruler` implementation is sufficient for small installations running relatively simple or inexpensive queries.

**Pros**:
- Nothing to be done

**Cons:**
- Loki's `ruler` will remain unreliable and inefficient when used in large multi-tenant environments with expensive queries.

### Proposal 1: Remote Execution

Taking inspiration from [Grafana Mimir's implementation](/docs/mimir/latest/operators-guide/architecture/components/ruler/#remote), the `ruler` would be configured to send its rule query to the `query-frontend` component over gRPC. The `querier` instances receiving queries from the `query-frontend` (or optionally via the `query-scheduler`) will handle the request and send the responses to the `query-frontend` and be combined. The `ruler` will receive and process these responses as if the query had been executed locally.

**Pros:**
- Takes full advantage of Loki's query acceleration techniques, leading to faster and more efficient rule evaluation
- Operationally simple as existing `query-frontend`/`query-scheduler`/`querier` setup can be used
- Per-tenant isolation available in Loki's query path (shuffle-sharding, per-tenant queues) can be used to reduce or eliminate the noisy neighbour problem

**Cons:**
- Increased interdependence in components, increased cross-component networking
- Reusing the same `query-frontend`/`query-scheduler`/`querier` setup can cause expensive queries to starve rule evaluations of query resources, and vice versa
  - Additional complexity introduced if this setup needs to be duplicated for rule evaluations (recommended: see **Other Notes** section below)

## Other Notes

If this feature were to be used in conjunction with [rule-based sharding](https://github.com/grafana/loki/pull/8092), this can present some further optimisation but also some additional challenges to consider.

> Aside: the `ruler` shards by rule group by default, which means that rules can be unevenly balanced across `ruler` instances if some rule groups have more expensive queries than others. Another consequence of this is that rule groups execute sequentially, so expensive queries can cause subsequent rules in the group to be delayed or even missed. Rule groups are evaluated concurrently.

Rule-based sharding distributes rules evenly across all available `ruler` instances, each in their own rule group. Consequentially, each rule that belongs to a `ruler` instance will be evaluated concurrently (as they're each in their own rule group). For tenants with hundreds or thousands of rules, this can result in large batches of queries being sent to the `query-frontend` in quick succession, should they all use the same interval or happen to overlap.

Assuming the remote rule evaluation takes place on the same read path that is used to execute tenant queries, care must be taken by operators who run large multi-tenant setups to ensure that large volumes of queries can be received, queued, and processed in an acceptable timeframe. The `query-scheduler` component is highly recommended in these situations, as it will enable the `query-frontend` and `query` components to scale out to accommodate the load. Shuffle-sharding should also be implemented to ensure that tenants with particularly large workloads do not starve out the query resources of other tenants. Alerting should also be put in place to notify operators if rule evaluations are being routinely missed or a tenants' query queues become full.

If rule evaluations and tenant queries are slowing each other down, the read path setup would need to be duplicated so that tenant queries and rule evaluations would not share the same query execution resources.

Rule-based sharding and remote evaluation can (and should) be implemented separately. Operators should first implement remote evaluation to improve `ruler` reliability, and _then_ further investigate rule-based sharding if rule evaluations are still being missed due to the sequential execution of rule groups, or advise their tenants to split these rule groups up.
