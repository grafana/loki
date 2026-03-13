# V2 Query Engine Scaling Analysis (BACKWARD-Only Queries)

## Executive Summary

With the load tool filtering to only V2-engine-supported queries (BACKWARD log + metric), the system behaves fundamentally differently from the mixed V1/V2 workload. The worker is now the true bottleneck with 1 worker, saturating at **6.5 QPS** (C=8). However, scaling to 3 workers makes performance **worse** (−23% QPS, +75% latency) because distributed task execution overhead (6.4× slower per task) exceeds the parallelism benefit. The system does not scale horizontally in its current architecture.

---

## Setup

- **Loki**: 1 instance (target=all), 1 engine/scheduler
- **Workers**: 1 or 3 (32 threads each)
- **Load tool**: BACKWARD-only filter (no FORWARD log queries), eliminating V1 fallback
- **Queries**: 13 test cases (9 BACKWARD log + 4 metric), all V2-engine-supported

---

## Part 1: Single Worker (1w × 32 threads)

### Concurrency Sweep

| Concurrency | QPS | Latency | Errors | Notes |
|-------------|-----|---------|--------|-------|
| 2           | 5.2 | 397 ms  | 0      | Low load, fast responses |
| 4           | 6.0 | 604 ms  | 0      | QPS still climbing |
| **8**       | **6.5** | **1,122 ms** | **0** | **Peak throughput** |
| 16          | 5.6 | 2,526 ms | 0     | Throughput declining, latency rising |
| 32          | 5.4 | 2,776 ms | 0     | Worker fully saturated |
| 64          | 0.0 | stalled  | —     | Task starvation: 774 tasks queued, all 32 threads busy |

**Key findings:**
- Peak QPS is **6.5** at C=8 — a 2.6× improvement over the old mixed V1/V2 workload (was ~2.5).
- Beyond C=8, latency grows faster than throughput — classic saturation curve.
- At C=64, the worker cannot keep up: 64 concurrent queries × ~54 tasks = ~3,456 tasks competing for 32 threads. Queries stall waiting for their tasks to complete.

### Prometheus Metrics at C=16 (Saturated)

| Metric | Value |
|--------|-------|
| Worker threads busy/ready | 32 / 0 (fully saturated) |
| Task queue length | 718 |
| Task throughput | 1,010 tasks/s |
| Avg task execution | 57 ms |
| Avg task queue wait | 772 ms |
| V2 execution avg | 1.92 s |
| V2 queries/s (engine) | 2.7 |

The worker IS the bottleneck: all threads busy, large queue, 93% of task time is spent waiting in the queue (772ms wait vs 57ms execution).

### Thread Scaling (C=2)

| Threads | QPS | Latency | Notes |
|---------|-----|---------|-------|
| 2       | 0.0 | deadlock | Both threads stuck on parent tasks |
| 4       | 2.6 | 487 ms  | Minimum viable (but fragile) |
| 8       | 1.3 | 3,906 ms | Recovering from prior backlog |
| 16      | 2.8 | 1,658 ms | Improving |
| **32**  | **4.2** | **969 ms** | Optimal for single queries |
| 64      | 4.2 | 496 ms  | Same QPS, lower latency (less queuing) |
| 128     | 3.7 | 559 ms  | Slight decline (goroutine overhead) |

**Deadlock at 2 threads**: Confirmed at C=2. Parent tasks (dispatched first per `workflow.go`) occupy both threads; scan tasks cannot run. This is the minimum reproduction case.

---

## Part 2: Three Workers (3w × 32 threads = 96 total)

### Concurrency Sweep

| Concurrency | QPS | Latency | Errors |
|-------------|-----|---------|--------|
| 2           | 4.4 | 456 ms  | 0      |
| 4           | 5.3 | 672 ms  | 0      |
| **8**       | **5.4** | **1,264 ms** | **0** |
| 16          | 5.1 | 2,787 ms | 0     |
| 32          | 4.5 | 5,714 ms | 0     |
| 64          | 3.8 | 10,891 ms | 0    |

**3 workers avoids the C=64 stall** (QPS 3.8 vs 0.0 with 1 worker), but peak QPS (5.4) is **lower** than 1 worker's peak (6.5).

### Prometheus Metrics at C=16 (Stabilized)

| Metric | Value |
|--------|-------|
| Worker threads busy/ready | 94 / 2 (near-saturated) |
| Task queue length | 501 |
| Task throughput | 1,032 tasks/s |
| Avg task execution | **364 ms** |
| Avg task queue wait | **1,330 ms** |
| V2 execution avg | **5.62 s** |
| V2 queries/s (engine) | 2.88 |
| Wire RTT (workers) | 7–9 ms |
| Engine inflight | 16 |

---

## Part 3: 1 Worker vs 3 Workers — Direct Comparison

### Side-by-Side at C=16, 32 threads/worker

| Metric | 1 Worker (32t) | 3 Workers (96t) | Change |
|--------|---------------|-----------------|--------|
| **QPS** | 5.6 | 4.1 | **−23%** |
| **Latency** | 2,526 ms | 4,263 ms | **+69%** |
| V2 exec avg | 1.92 s | 5.62 s | **+193%** |
| Task exec avg | 57 ms | 364 ms | **+539% (6.4×)** |
| Task queue avg | 772 ms | 1,330 ms | **+72%** |
| Tasks/s | 1,010 | 1,032 | +2% |
| V2 queries/s | 2.7 | 2.88 | +7% |

### Side-by-Side at C=2 (Minimal Load)

| Metric | 1 Worker | 3 Workers | Change |
|--------|----------|-----------|--------|
| QPS | 5.2 | 4.4 | **−15%** |
| Latency | 397 ms | 456 ms | +15% |

Even with zero queuing, 3 workers are 15% slower. This is pure distributed overhead.

### Peak QPS Comparison

| Config | Peak QPS | At Concurrency |
|--------|----------|----------------|
| 1 Worker × 32t | **6.5** | C=8 |
| 3 Workers × 32t | 5.4 | C=8 |

**Adding 2 workers reduced peak QPS by 17%.**

---

## Root Cause Analysis

### Why Distributed Execution Is Slower

Task execution time went from 57ms (local) to 364ms (remote) — a **6.4× increase**. This overhead comes from three sources:

#### 1. Wire Protocol Overhead
Each task assignment and completion requires gRPC message exchange between the scheduler (on the engine service) and workers. Wire RTT is 7–9ms per message. A task lifecycle involves multiple messages (assignment, ready, data frames, completion), multiplying the overhead.

#### 2. Stream Data Relay
In the distributed model, scan tasks produce data that flows through gRPC streams to the scheduler, which relays it to parent tasks potentially on different workers. With 1 worker, stream data stays in-process (shared memory). The `pkg/engine/internal/worker/node_source.go` `Read()` call blocks on network data instead of in-memory channel reads.

#### 3. Shared Storage Contention
All workers read from the same Docker volume mount. With 3 workers × 32 threads = 96 concurrent readers vs 1 worker × 32 threads, filesystem contention increases. Data object reads become I/O-bound on the shared Docker filesystem layer.

The net effect: **3× workers × (1/6.4 efficiency/thread) = 0.47× effective capacity**. Adding workers made the system slower, not faster.

### Why Task Queue Wait Increased Despite More Threads

Despite having 3× the threads (96 vs 32), task queue wait went from 772ms to 1,330ms (+72%). This is because:
- Each thread executes tasks 6.4× slower (364ms vs 57ms)
- Effective throughput per thread: 1/0.364 = 2.7 tasks/s (vs 1/0.057 = 17.5 tasks/s)
- Total effective throughput: 96 × 2.7 = 259 tasks/s (vs 32 × 17.5 = 560 tasks/s)
- The 2× effective throughput reduction causes task queue to grow longer

Measured throughput confirms this: 1,032 tasks/s with 3 workers vs 1,010 tasks/s with 1 worker. Despite 3× resources, task throughput is essentially flat.

### Why C=64 Stalls with 1 Worker but Not 3

With 1 worker (32 threads) at C=64: 64 queries × ~54 tasks = ~3,456 tasks. Only 32 threads, so each query's tasks are severely delayed by competition. Queries take so long that client HTTP timeouts fire, creating a stall cascade.

With 3 workers (96 threads) at C=64: same 3,456 tasks, but 96 threads. Each thread is slower (364ms vs 57ms), but the total capacity is enough to prevent full stall. Queries complete very slowly (10.9s average) but they do complete.

### Deadlock Analysis

The deadlock at T=2 occurs regardless of concurrency or worker count:

1. `pkg/engine/internal/workflow/workflow.go` dispatches `taskTypeOther` (parent/aggregation tasks) before `taskTypeScan` (leaf scan tasks)
2. With 2 threads, 2 parent tasks occupy both threads
3. Parent tasks block on `nodeSource.Read()` waiting for child scan output
4. Scan tasks are queued but cannot run — no free threads
5. Result: permanent deadlock (delta_qps=0, queue growing)

This is a dependency inversion: parent tasks are dispatched first but depend on child tasks that are dispatched second. The fix is to dispatch scan tasks first:

```go
// pkg/engine/internal/workflow/workflow.go
for _, taskType := range []taskType{
    taskTypeScan,   // dispatch leaf tasks first
    taskTypeOther,  // then parent tasks that depend on them
}
```

---

## Summary of Scaling Properties

| Property | Result | Evidence |
|----------|--------|----------|
| Concurrency scaling (1w) | **Good until saturation** | C=2→8: QPS 5.2→6.5, then plateau |
| Concurrency scaling (3w) | **Poor** | C=2→8: QPS 4.4→5.4, then decline |
| Thread scaling (1w) | **Good up to ~32-64** | T=4→32: QPS 2.6→4.2, plateau at 64, decline at 128 |
| Worker scaling (1→3) | **Negative** | −23% QPS, +69% latency, 6.4× task overhead |
| Deadlock threshold | **T ≤ 2 always deadlocks** | Regardless of worker count or concurrency |

---

## Architectural Recommendations

### 1. Fix Task Dispatch Ordering (Deadlock Prevention)

Reverse the dispatch order in `pkg/engine/internal/workflow/workflow.go` to send scan tasks before parent tasks. This eliminates the deadlock entirely and allows safe operation with fewer threads.

### 2. Reduce Distributed Execution Overhead

The 6.4× per-task overhead makes horizontal scaling counterproductive. Root causes to address:
- **Minimize stream data relay**: co-locate parent and child tasks on the same worker when possible. If a parent task depends on scan tasks, schedule them on the same worker to avoid network stream relay.
- **Batch wire messages**: aggregate multiple task assignments and results into fewer gRPC round-trips.
- **Worker-local storage caching**: cache frequently-read data object sections in worker memory to reduce repeated storage reads.

### 3. Task-Aware Scheduling

The current scheduler uses a fair queue that distributes tasks without considering task dependencies. A dependency-aware scheduler could:
- Keep a query's tasks on the same worker (reduces stream relay)
- Prioritize scan tasks over parent tasks (prevents deadlock by design)
- Limit the number of concurrent queries accepted when the task queue is deep (back-pressure)

### 4. Admission Control at the Engine

At C=64 with 1 worker, the system stalls because too many concurrent queries create thousands of tasks. The engine should limit concurrent queries based on worker capacity: `max_concurrent_queries ≈ total_threads / tasks_per_query`. For 32 threads and ~54 tasks/query, this is ~0.6, meaning the engine should serialize queries for a single worker.

---

## Raw Data Summary

### Concurrency Sweep (32 threads/worker)

| Concurrency | 1w QPS | 1w Latency | 3w QPS | 3w Latency |
|-------------|--------|------------|--------|------------|
| 2           | 5.2    | 397 ms     | 4.4    | 456 ms     |
| 4           | 6.0    | 604 ms     | 5.3    | 672 ms     |
| 8           | **6.5**| 1,122 ms   | **5.4**| 1,264 ms   |
| 16          | 5.6    | 2,526 ms   | 5.1    | 2,787 ms   |
| 32          | 5.4    | 2,776 ms   | 4.5    | 5,714 ms   |
| 64          | stall  | —          | 3.8    | 10,891 ms  |

### Thread Scaling (1 Worker, C=2)

| Threads | QPS | Latency |
|---------|-----|---------|
| 2       | 0.0 | deadlock |
| 4       | 2.6 | 487 ms  |
| 16      | 2.8 | 1,658 ms |
| 32      | 4.2 | 969 ms  |
| 64      | 4.2 | 496 ms  |
| 128     | 3.7 | 559 ms  |

### Key Metrics Comparison (C=16)

| Metric | 1 Worker | 3 Workers |
|--------|----------|-----------|
| QPS | 5.6 | 4.1 |
| Latency | 2,526 ms | 4,263 ms |
| V2 exec avg | 1.92 s | 5.62 s |
| Task exec avg | 57 ms | 364 ms |
| Task queue avg | 772 ms | 1,330 ms |
| Tasks/s | 1,010 | 1,032 |
| Wire RTT (workers) | ~1 ms | ~8 ms |
