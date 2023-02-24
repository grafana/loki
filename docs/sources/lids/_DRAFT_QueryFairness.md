---
title: "XXXX: Query fairness across users within tenants"
description: "Improve the query scheduler to ensure fairness between individual users of individual tenants."
---

# XXXX: Query fairness across users within tenants


**Author:** Christian Haudum (<christian.haudum@grafana.com>)

**Date:** 02/2023

**Sponsor(s):** @chaudum @owen-d

**Type:** Feature

**Status:** Draft

**Related issues/PRs:**

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):**

---

## Background

The query scheduler (or short scheduler) is a component of Loki that distributes requests (sub-queries) from the query frontend (or short frontend) to the querier workers so that execution fairness between tenants can be guaranteed.

By maintaining separate FIFO queues for each tenant and assigning the correct amount of querier workers to these queues, the scheduler takes care that a single tenant cannot compromise all other tenants' query capabilities.

**Component diagram:**

```plantuml
skinparam componentStyle rectangle

package Frontend {
    () RoundTripper as QF
}

package Scheduler {
    () FrontendLoop as FL

    component RequestQueue {
        [Tenant Queue] as T1
        [Tenant Queue] as T2
    }

    () QuerierLoop as Q1
    () QuerierLoop as Q2

}

QF -->> FL
FL -->> T1
FL -->> T2

package Querier {
    [Worker] as W1
    [Worker] as W2
}

T1 -->> Q1
Q1 -->> W1

T2 -->> Q1
T2 -->> Q2
Q2 -->> W2
```

![component diagram](../scheduler-component-diagram.png)

**Sequence diagram:**

```plantuml
Client -> Frontend: GET /api/v1/query_range
activate Frontend

group gRPC stream [loop]
    Frontend -> FrontendLoop: SchedulerToFrontend{}
    FrontendLoop -> RequestQueue: Enqueue()
    RequestQueue -> tenantqueues: ->chan
    RequestQueue --> FrontendLoop: 
    FrontendLoop --> Frontend: FrontendToScheduler{}
end

group gRPC stream [loop]
    QuerierLoop -> RequestQueue: GetNextRequestForQuerier()
    tenantqueues -> RequestQueue: <-chan
    RequestQueue --> QuerierLoop: Request{}
    QuerierLoop -> Querier: SchedulerToQuerier{}
    Querier --> QuerierLoop: QuerierToScheduler{}
end

Frontend --> Frontend: wait
Querier -> Frontend: QueryResult{}
Frontend --> Client: query response {...}
deactivate Frontend
```

![sequence diagram](../scheduler-sequence-diagram.png)

## Problem Statement

Even though Loki is built as multi-tenant system by default, there are use-cases where a Loki installation only has a very large, single tenant, e.g. dedicated Loki cells for customers in Grafana Cloud.

However, there are potentially a lot of different users using the same tenant to query logs, such as users accessing Loki from Grafana or via CLI or HTTP API. This can lead to contention between queries of different users, because they all share the same tenant.

While the current implementation of the scheduler queues allows for QoS guarantees between tenants, it does not account for QoS guarantees across individual users within a single tenant.

That said, Loki does not have the notation of individual users.

## Goals

The main goal of the following proposals is to lay out ideas how to improve the scheduler component to not only assure QoS across tenants, but also across actors (users) within a tenant, without requiring any changes to the deployment model of frontend, scheduler and queriers.
This should also include changes to the queue structure to be easily extensible for future scheduling improvements. 

## Non-Goals (optional)

While changing and extending the scheduler requires also user-facing API changes, the public API is not part of the discussion of this document.

## Proposals

### Proposal 0: Do nothing

An alternative to changing the scheduling mechanism is to handle QoS control via multiple tenants and multi-tenant querying.

**Pros:**
* Keeps the scheduler as simple as it is now
* No development time

**Cons:**
* While that separation into tenants may work for some prospects, it might not be feasible to implement for others.

### Proposal 1: Add fixed second level to scheduler

The current scheduler is implemented in a way that it maintains a separate FIFO queue for each tenant. When a request (sub-query) is enqueued, the scheduler puts it into the existing queue for that tenant. If the queue does not exist yet, it creates it first and re-assignes the connected querier workers to the available tenant queues. Each querier worker pulls round-robin from the assigned queues in a loop.

Now, instead of enqueuing and pulling directly from the per-tenant queue, requests get enqueued in per-user queues and the per-tenant queue pulls round-robin from the user queues that are assigned to the tenant queues.

**Component diagram:**

```plantuml
skinparam componentStyle rectangle

package Frontend {
    () RoundTripper as QF
}

package Scheduler {
    () FrontendLoop as FL

    component RequestQueue {
        [User Queue] as U1
        [User Queue] as U2
        [User Queue] as U3
        [User Queue] as U4
        [Tenant Queue] as T1
        [Tenant Queue] as T2
    }

    () QuerierLoop as Q1
    () QuerierLoop as Q2

    U1 ..>> T1
    U2 ..>> T2
    U3 ..>> T2
    U4 ..>> T2
}

QF -->> FL
FL -->> U1
FL -->> U2
FL -->> U3
FL -->> U4

package Querier {
    [Worker] as W1
    [Worker] as W2
}

T1 -->> Q1
Q1 -->> W1

T2 -->> Q1
T2 -->> Q2
Q2 -->> W2
```

![component diagram](../scheduler-proposal-1-component-diagram.png)

Like the current implementation, the scheduler enqueues requests based on the `X-Scope-OrgID` header (or equivalent key in the request context), but also takes a second key (such as `X-Scope-UserID`) into account. This ensembles a fixed hierarchy with two levels where the tenant-to-user relation is a one-to-many relation.
However, this has the disadvantage that the concept of users (that does not exist yet in Loki) leaks into the scheduler domain.

**Pros:**
* Relatively simple to to implement

**Cons:**
* Not extensible
* Leaks domain knowledge

### Proposal 2: Fully hierarchical scheduler

This proposal is similar to _Proposal 1_, but with the difference that there are no fixed levels and levels can be nested arbitrarily.

**Component diagram:**

```plantuml
skinparam componentStyle rectangle

package Frontend {
    () RoundTripper as QF
}

package Scheduler {
    () FrontendLoop as FL

    component RequestQueue {
        [L3 Queue] as Z1
        [L3 Queue] as Z2
        [L3 Queue] as Z3
        [L3 Queue] as Z4
        [L2 Queue] as Y1
        [L2 Queue] as Y2
        [L2 Queue] as Y3
        [L1 Queue] as X1
        [L1 Queue] as X2
        [L1 Queue] as X3
        [L0 Queue] as R1
        [L0 Queue] as R2
    }

    () QuerierLoop as Q1
    () QuerierLoop as Q2

    Z1 ..>> Y1
    Z2 ..>> Y1
    Z3 ..>> Y2
    Z4 ..>> Y2
    Y1 ..>> X1
    Y2 ..>> X2
    Y3 ..>> X2
    X1 ..>> R1
    X2 ..>> R2
    X3 ..>> R2
}

QF -->> FL
FL -->> Z1
FL -->> Z2
FL -->> Z3
FL -->> Z4
FL -->> Y3
FL -->> X2
FL -->> X3

package Querier {
    [Worker] as W1
    [Worker] as W2
}

R1 -->> Q1
Q1 -->> W1

R2 -->> Q1
R2 -->> Q2
Q2 -->> W2
```

![component diagram](../scheduler-proposal-2-component-diagram.png)

The implementation of the `RequestQueue`, which controls what querier workers are connected to which root queues (aka tenant queues), can be kept as is. However, the concept of tenants and users is dropped and replaced by by a concept of hierarchical actors, which can be represented as a slice of identifiers. Note, this does **not** drop the concept of tenants throughout Loki (represented in the `X-Scope-OrgID` header and/or request context).

**Example of identifiers:**

```go
actorA := []string{"tenant_a", "user_1"}
actorB := []string{"tenant_b", "user_2"}
actorC := []string{"tenant_b", "user_3", "service_foo"}
actorD := []string{"tenant_b", "user_3", "service_bar"}
```

More generally:

```go
actorN := []string{"L0 Queue", "L1 Queue", "L2 Queue", ... "Ln Queue"}
```

The L0 queue (root queue) needs to be able to handle worker connections and therefore needs additional functionality compared to its leaf queues.

The following code snippet is meant to show the simplified recursive structure of the queues.

```go
type Request interface{}

type Queue interface {
    Deqeue(actor []string) Request
    Enqueue(r Request, actor []string) error
}

// RequestQueue implements Queue
type RequestQueue struct {
    queriers   map[string]*querier
    rootQueues map[string]*RootQueue
}

// RootQueue implements Queue
type RootQueue struct {
    queriers map[string]*querier
    leafs    map[string]*LeafQueue
    ch       chan Request
}

// LeafQueue implements Queue
type LeafQueue struct {
    leafs map[string]*LeafQueue
    ch    chan Request
}
```

**Pros:**
* Backwards compatible, because tenant can be identified as `[]string{"tenantID"}`
* Queue hierarchy can be extended without changing the scheduler implementation
* Implementation does not require knowledge outside of its domain

**Cons:**
* More complex to implement than fixed amount of levels
* Each queue comes with memory overhead

## Other Notes
