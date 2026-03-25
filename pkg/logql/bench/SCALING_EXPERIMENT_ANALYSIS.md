# Loki V2 Query Engine: Scaling Investigation

## 1. Background

The Loki V2 query engine introduces a distributed execution model where LogQL queries are decomposed into a DAG of tasks (scan, aggregation, sort) and distributed across worker nodes for parallel execution. The architecture separates concerns into three services:

- **Engine** (`query-engine` + `query-engine-scheduler`): receives queries, builds logical and physical plans, creates a task workflow, and coordinates execution through a scheduler.
- **Workers** (`query-engine-worker`): connect to the scheduler over gRPC, pull tasks from a fair queue, execute them (reading data objects from storage), and stream results back.
- **Loki frontend** (`target=all`): receives client HTTP requests and routes V2-supported queries to the engine service via an engine router middleware.

This investigation evaluates whether the V2 engine scales effectively when we add more worker threads or more worker nodes.

---

## 2. Test Environment

### 2.1 Docker Compose Setup

The benchmark environment runs all services via Docker Compose on a single host:

| Service | Target | Description |
|---------|--------|-------------|
| `loki` | `target=all` | Full Loki stack; query frontend with engine router forwards V2 queries to the engine service |
| `engine` | `query-engine,query-engine-scheduler` | V2 engine + scheduler; receives queries from the frontend, plans them, dispatches tasks |
| `worker` (1 or 3 replicas) | `query-engine-worker` | Connects to the scheduler, executes tasks; configurable thread count |
| `prometheus` | — | Scrapes all services for metrics |
| `grafana` | — | Dashboards |

Workers are configured with `query_engine.distributed: true` and `query_engine.scheduler_lookup_address: dns+engine:3100`, discovering the scheduler via DNS. Each worker starts with `worker_threads: 32` goroutines that pull and execute tasks.

Storage is a shared Docker volume mount containing a pre-generated ~2 GB dataset of log data with structured metadata.

### 2.2 Dynamic Worker Thread Control

To test different thread counts without restarting services, we implemented a runtime HTTP API on each worker. The `SetNumThreads(n)` method on the internal worker dynamically scales the goroutine pool: it spins up new goroutines (each with its own cancellable context) when scaling up, and cancels excess goroutines when scaling down. The `ThreadsHandler` HTTP endpoint exposes this:

```
GET  /query-engine/worker/threads        → returns current thread count
POST /query-engine/worker/threads        → sets thread count (form field: worker_threads)
```

To change threads on all workers at once:

```bash
for container in $(docker compose ps -q worker); do
  docker exec "$container" curl -s -X POST \
    -d 'worker_threads=64' http://localhost:3100/query-engine/worker/threads
done
```

Worker replicas are scaled via Docker Compose:

```bash
docker compose up -d --scale worker=3    # scale to 3 workers
docker compose up -d --scale worker=1    # scale back to 1
```

### 2.3 Load Generation Tool

We developed a purpose-built load generator (`pkg/logql/bench/cmd/load`) that sends a continuous stream of LogQL queries to Loki. Key features:

- **Query suite**: loads query definitions from YAML files (suites: `fast`, `regression`, `exhaustive`), resolves template variables (`${SELECTOR}`, `${RANGE}`) against the dataset metadata, and expands them into concrete test cases with time ranges and directions.
- **V2-only filtering**: the tool filters out `FORWARD` log queries (which are not supported by the V2 engine's logical planner), ensuring all generated load exercises the V2 path.
- **Dynamic concurrency**: the number of concurrent query goroutines can be changed at runtime via an HTTP API, without restarting the tool.
- **Stats API**: exposes a `/stats` endpoint returning a JSON history of recent performance snapshots (QPS, latency, error counts, per-interval deltas).

Running the load tool:

```bash
go run ./cmd/load/main.go -concurrency=2 -suite=fast -addr=http://localhost:3100
```

Changing concurrency and reading stats at runtime:

```bash
curl -X POST -d 'concurrency=16' http://localhost:8080/concurrency
curl http://localhost:8080/stats?n=10
```

The tool calculates QPS based on successful (HTTP 200) responses only.

---

## 3. Query Execution Path

Understanding the query path is essential to interpreting the results.

```
Client HTTP request
  → Loki (target=all, port 3100)
    → Query Frontend middleware
      → Engine Router
        ├─ V2 supported? → DownstreamRoundTripper → HTTP → Engine service
        │                    → Logical Plan → Physical Plan → Workflow
        │                      → Scheduler dispatches ~54 tasks per query
        │                        → Workers execute tasks, stream results back
        │                          → Engine collects results, returns HTTP response
        └─ Not V2? → V1 chunks engine path (not relevant with our filtered load)
```

Each query produces approximately **54 tasks**: ~50 scan tasks (one per data object section in the dataset) plus 1–3 parent aggregation tasks and 1 root task. Tasks have dependencies: parent tasks block on `nodeSource.Read()` waiting for child scan tasks to produce data through streams.

The scheduler assigns tasks from a fair queue without dependency awareness. Critically, the workflow dispatcher in `pkg/engine/internal/workflow/workflow.go` sends parent tasks (`taskTypeOther`) **before** scan tasks (`taskTypeScan`).

---

## 4. Findings

### 4.1 Single Worker: Concurrency Scaling

**Configuration**: 1 worker, 32 threads, varying client concurrency.

| Concurrency | QPS | Avg Latency | Errors |
|-------------|-----|-------------|--------|
| 2           | 5.2 | 397 ms      | 0      |
| 4           | 6.0 | 604 ms      | 0      |
| **8**       | **6.5** | **1,122 ms** | **0** |
| 16          | 5.6 | 2,526 ms    | 0      |
| 32          | 5.4 | 2,776 ms    | 0      |
| 64          | 0.0 | stalled     | —      |

**Observations**:

- **Peak throughput is 6.5 QPS at C=8.** Beyond this, the worker is saturated: all 32 threads busy, 718 tasks queued. Adding more clients only increases latency without improving throughput — the classic saturation curve.
- **At C=64 the system stalls.** 64 concurrent queries produce ~3,456 tasks competing for 32 threads. Queries take so long that client HTTP timeouts fire, creating a cascade where no queries complete.
- **93% of task time is queue wait.** At C=16, average task execution is 57 ms but average queue wait is 772 ms. The worker is the clear bottleneck.

Prometheus metrics at C=16 (saturated):

| Metric | Value |
|--------|-------|
| Worker threads (busy / ready) | 32 / 0 |
| Task queue length | 718 |
| Task throughput | 1,010 tasks/s |
| Avg task execution | 57 ms |
| Avg task queue wait | 772 ms |
| V2 query execution avg | 1.92 s |

### 4.2 Single Worker: Thread Scaling

**Configuration**: 1 worker, varying thread count, client concurrency=2 (to avoid saturation effects).

| Threads | QPS | Avg Latency | Notes |
|---------|-----|-------------|-------|
| 2       | 0.0 | —           | Deadlock |
| 4       | 2.6 | 487 ms      | Minimum viable |
| 16      | 2.8 | 1,658 ms    | |
| 32      | 4.2 | 969 ms      | |
| 64      | 4.2 | 496 ms      | Same QPS, lower latency |
| 128     | 3.7 | 559 ms      | Slight decline |

**Observations**:

- **Threads scale well from 4 to 32**, roughly doubling QPS.
- **32–64 threads is the sweet spot.** Beyond 64, goroutine scheduling overhead causes a slight decline.
- **2 threads always deadlocks** (see Section 4.4).

### 4.3 Three Workers: Does Horizontal Scaling Help?

**Configuration**: 3 workers × 32 threads (96 total), varying client concurrency.

| Concurrency | 1w QPS | 1w Latency | 3w QPS | 3w Latency |
|-------------|--------|------------|--------|------------|
| 2           | 5.2    | 397 ms     | 4.4    | 456 ms     |
| 4           | 6.0    | 604 ms     | 5.3    | 672 ms     |
| 8           | **6.5**| 1,122 ms   | **5.4**| 1,264 ms   |
| 16          | 5.6    | 2,526 ms   | 5.1    | 2,787 ms   |
| 32          | 5.4    | 2,776 ms   | 4.5    | 5,714 ms   |
| 64          | stall  | —          | 3.8    | 10,891 ms  |

**Observations**:

- **3 workers is slower than 1 worker at every concurrency level.** Peak QPS dropped from 6.5 to 5.4 (−17%). At C=16, QPS dropped from 5.6 to 4.1 (−23%).
- **Even at C=2 (no queuing), 3 workers are 15% slower.** This is pure distributed overhead — not a load effect.
- **The one benefit**: 3 workers avoid the C=64 stall (3.8 QPS vs 0.0), because 96 threads can absorb more concurrent tasks than 32.

Prometheus metrics at C=16 — direct comparison:

| Metric | 1 Worker (32t) | 3 Workers (96t) | Change |
|--------|---------------|-----------------|--------|
| QPS | 5.6 | 4.1 | **−23%** |
| Avg Latency | 2,526 ms | 4,263 ms | **+69%** |
| V2 query exec avg | 1.92 s | 5.62 s | **+193%** |
| Avg task execution | 57 ms | 364 ms | **+539% (6.4×)** |
| Avg task queue wait | 772 ms | 1,330 ms | **+72%** |
| Task throughput | 1,010 tasks/s | 1,032 tasks/s | +2% |
| Wire RTT (workers) | ~1 ms | ~8 ms | +700% |

**The core problem**: individual task execution ballooned from 57 ms to 364 ms — a **6.4× slowdown** — with remote workers. Despite 3× the threads, total task throughput is essentially flat (1,010 vs 1,032 tasks/s). The math: 3× threads × (1/6.4 per-thread efficiency) = 0.47× effective capacity. Adding workers made the system slower.

### 4.4 Deadlock at Low Thread Counts

**Observed**: at 2 threads per worker, the system deadlocks at any concurrency, including C=2.

**Root cause** (from source code analysis):

1. The workflow dispatcher (`pkg/engine/internal/workflow/workflow.go`) sends `taskTypeOther` (parent/aggregation tasks) before `taskTypeScan` (leaf scan tasks).
2. Parent tasks are assigned to the 2 available threads.
3. Parent tasks block on `nodeSource.Read()` (`pkg/engine/internal/worker/node_source.go`), waiting for child scan tasks to produce data via streams.
4. Scan tasks sit in the scheduler queue — but no threads are free to run them.
5. Classic deadlock: parents wait for children that can never execute.

**Evidence**: both threads show as BUSY, task queue grows (51+ tasks), QPS drops to exactly 0.0 and never recovers until threads are increased.

This deadlock occurs regardless of the number of workers or the concurrency level. With 3 workers × 2 threads (6 total), the same deadlock happens because parent tasks from concurrent queries can fill all 6 threads.

---

## 5. Root Cause Analysis

### 5.1 Why Distributed Execution Is Slower

The 6.4× per-task overhead with remote workers comes from three sources:

**Wire protocol overhead**: each task lifecycle involves multiple gRPC messages between the scheduler (engine service) and workers — assignment, ready signals, data frames, completion. The measured wire RTT is 7–9 ms per message for remote workers vs ~1 ms in-process. Multiple round-trips per task multiply this cost.

**Stream data relay**: in the V2 engine, scan tasks produce data that flows through streams to parent tasks. With 1 worker, streams are in-process shared memory. With multiple workers, stream data travels: worker (scan) → gRPC → scheduler → gRPC → worker (parent). The `nodeSource.Read()` call blocks on network I/O instead of channel reads.

**Shared storage contention**: all worker containers read from the same Docker volume mount. 96 concurrent readers (3 workers × 32 threads) contend more heavily than 32 readers on the shared filesystem layer.

### 5.2 Why the Task Queue Gets Worse with More Workers

Despite 3× the threads (96 vs 32), average task queue wait **increased** from 772 ms to 1,330 ms (+72%). This is because each thread's effective throughput dropped:

- 1 worker: 1/0.057s = 17.5 tasks/thread/s → 32 threads × 17.5 = 560 effective tasks/s
- 3 workers: 1/0.364s = 2.7 tasks/thread/s → 96 threads × 2.7 = 259 effective tasks/s

The measured throughput confirms this: ~1,010 tasks/s with both configurations. Adding workers traded per-thread efficiency for thread count — and lost.

### 5.3 Why C=64 Stalls with 1 Worker

With 32 threads and 64 concurrent queries generating ~3,456 tasks, there are ~108 tasks per thread. Each query's ~54 tasks are interleaved with thousands of others in the fair queue. A query cannot complete until all its tasks finish, but its tasks are scattered across the queue behind other queries' tasks. Completion time grows super-linearly until HTTP client timeouts (5 minutes) fire, creating a stall cascade.

With 3 workers (96 threads), the same 3,456 tasks have ~36 tasks per thread. Queries complete slowly (10.9 s avg latency) but they do complete — the higher thread count prevents the stall.


---

## 6. Summary

| Property | Result |
|----------|--------|
| Concurrency scaling (1 worker) | Good until saturation at C=8 (peak 6.5 QPS) |
| Thread scaling (1 worker) | Good from 4→32 threads; plateau at 64; decline at 128 |
| Horizontal scaling (1→3 workers) | **Negative**: −23% QPS, +69% latency |
| Per-task remote overhead | **6.4× slower** than local execution |