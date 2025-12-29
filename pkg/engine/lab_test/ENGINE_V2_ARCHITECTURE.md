# Loki Query Engine V2 Architecture

This document provides a comprehensive overview of the Loki Query Engine V2, an experimental
query execution engine that provides columnar data processing using Apache Arrow.

## Table of Contents

1. [Overview](#overview)
2. [Query Execution Pipeline](#query-execution-pipeline)
3. [Logical Planning](#logical-planning)
4. [Physical Planning](#physical-planning)
5. [Workflow System](#workflow-system)
6. [Execution](#execution)
7. [Distributed Execution](#distributed-execution)
8. [Configuration](#configuration)
9. [Storage System](#storage-system)

---

## Overview

The Query Engine V2 is designed to process LogQL queries using a modern, columnar approach.
Unlike the V1 engine which processes logs row-by-row, V2 processes data in batches using
Apache Arrow RecordBatches, enabling vectorized operations and better CPU cache utilization.

### Key Components

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                                  Query Execution                                     │
├──────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                      │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐            │
│  │   LogQL     │    │  Logical    │    │  Physical   │    │  Workflow   │            │
│  │   Parser    │───▶│   Planner   │───▶│   Planner   │───▶│   Planner   │            │
│  │ (syntax.*)  │    │ (logical.*) │    │ (physical.*)│    │ (workflow.*)│            │
│  └─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘            │
│        │                  │                  │                  │                    │
│        ▼                  ▼                  ▼                  ▼                    │
│  syntax.Expr        logical.Plan      physical.Plan      workflow.Workflow           │
│  (AST)              (SSA IR)          (DAG)              (Task Graph)                │
│                                                                                      │
├──────────────────────────────────────────────────────────────────────────────────────┤
│                              Execution Layer                                         │
├──────────────────────────────────────────────────────────────────────────────────────┤
│                                                                                      │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐                               │
│  │  Scheduler  │◀──▶│   Worker    │◀──▶│  Executor   │                               │
│  │             │    │             │    │             │                               │
│  └─────────────┘    └─────────────┘    └─────────────┘                               │
│                                              │                                       │
│                                              ▼                                       │
│                                        Pipeline                                      │
│                                   (Arrow RecordBatch)                                │
│                                                                                      │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### Engine Variants

The V2 engine has two implementations:

1. **Basic Engine** (`basic_engine.go`): Sequential execution, no parallelism. Implements the
   `logql.Engine` interface for compatibility with the existing query path.

2. **Engine** (`engine.go`): Full-featured distributed execution with scheduler support.
   Adds workflow planning and distributed task scheduling.

---

## Query Execution Pipeline

A query goes through multiple transformation stages:

```
LogQL String
    │
    ▼ (syntax.ParseExpr)
syntax.Expr (AST)
    │
    ▼ (logical.BuildPlan)
logical.Plan (SSA IR)
    │
    ▼ (physical.Planner.Build)
physical.Plan (DAG)
    │
    ▼ (physical.Planner.Optimize)
physical.Plan (Optimized DAG)
    │
    ▼ (workflow.New)
workflow.Workflow (Task Graph)
    │
    ▼ (workflow.Run)
executor.Pipeline
    │
    ▼ (Pipeline.Read)
arrow.RecordBatch
    │
    ▼ (ResultBuilder)
logqlmodel.Result
```

### Stage Details

| Stage | Input | Output | Purpose |
|-------|-------|--------|---------|
| Parsing | LogQL string | `syntax.Expr` | Parse query string into AST |
| Logical Planning | `syntax.Expr` + params | `logical.Plan` | Convert AST to SSA intermediate representation |
| Physical Planning | `logical.Plan` | `physical.Plan` | Convert SSA to executable DAG with data sources |
| Optimization | `physical.Plan` | `physical.Plan` | Apply optimization passes (pushdowns) |
| Workflow Planning | `physical.Plan` | `workflow.Workflow` | Partition plan into distributable tasks |
| Execution | `workflow.Workflow` | `executor.Pipeline` | Execute tasks and stream results |
| Result Collection | `Pipeline` | `logqlmodel.Result` | Convert Arrow batches to LogQL result format |

---

## Logical Planning

The logical planner (`pkg/engine/internal/planner/logical`) converts the LogQL AST into a
Static Single Assignment (SSA) intermediate representation.

### SSA Form

SSA ensures each value is assigned exactly once, making data flow analysis easier.
Each instruction is assigned to a unique variable (`%1`, `%2`, etc.), and variables
are never reassigned. This makes data flow explicit and enables powerful optimizations.

### Example 1: Log Query with Label Selector and Line Filter

**LogQL Query:**
```logql
{cluster="prod", namespace=~"loki-.*"} |= "metric.go"
```

**SSA Representation:**
```
%1 = EQ label.cluster "prod"
%2 = MATCH_RE label.namespace "loki-.*"
%3 = AND %1 %2
%4 = MATCH_STR builtin.message "metric.go"
%5 = MAKETABLE [selector=%3, predicates=[%4], shard=0_of_1]
%6 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%7 = SELECT %5 [predicate=%6]
%8 = LT builtin.timestamp 1970-01-01T02:00:00Z
%9 = SELECT %7 [predicate=%8]
%10 = SELECT %9 [predicate=%4]
%11 = TOPK %10 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%12 = LOGQL_COMPAT %11
RETURN %12
```

**Line-by-Line Explanation:**

| Line | Instruction | Explanation |
|------|-------------|-------------|
| `%1 = EQ label.cluster "prod"` | **Stream selector predicate (equality)** - Creates a predicate that matches streams where the `cluster` label equals `"prod"`. The `label.` prefix indicates this is a stream label (not metadata or parsed). |
| `%2 = MATCH_RE label.namespace "loki-.*"` | **Stream selector predicate (regex)** - Creates a predicate that matches streams where the `namespace` label matches the regex `loki-.*`. Regex predicates use `MATCH_RE` instead of `EQ`. |
| `%3 = AND %1 %2` | **Combine predicates** - Combines the two stream selector predicates with logical AND. The result represents `{cluster="prod", namespace=~"loki-.*"}`. Multiple label matchers are always ANDed together. |
| `%4 = MATCH_STR builtin.message "metric.go"` | **Line filter predicate** - Creates a predicate for the line filter `\|= "metric.go"`. Uses `MATCH_STR` for substring matching. `builtin.message` is the special column containing the log line text. |
| `%5 = MAKETABLE [selector=%3, predicates=[%4], shard=0_of_1]` | **Define data source** - Creates the logical table (data source) with: the stream selector (`selector=%3`), line filter predicates (`predicates=[%4]`), and shard info (`shard=0_of_1` means no sharding, i.e., shard 0 of 1 total). This is the entry point for data. |
| `%6 = GTE builtin.timestamp 1970-01-01T01:00:00Z` | **Time range predicate (start)** - Creates a predicate for the query start time. `builtin.timestamp` is the special column for log entry timestamps. `GTE` = "greater than or equal to". |
| `%7 = SELECT %5 [predicate=%6]` | **Apply start time filter** - Filters the table from `%5` to only include rows where timestamp >= start time. `SELECT` is the filtering operation. |
| `%8 = LT builtin.timestamp 1970-01-01T02:00:00Z` | **Time range predicate (end)** - Creates a predicate for the query end time. `LT` = "less than" (end time is exclusive). |
| `%9 = SELECT %7 [predicate=%8]` | **Apply end time filter** - Filters the result from `%7` to only include rows where timestamp < end time. Now we have data within the time range. |
| `%10 = SELECT %9 [predicate=%4]` | **Apply line filter** - Filters the result from `%9` using the line filter predicate from `%4`. This applies the `\|= "metric.go"` filter to the log lines. |
| `%11 = TOPK %10 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]` | **Sort and limit** - For log queries, results must be sorted by timestamp. `TOPK` sorts by `builtin.timestamp`, returns top `k=1000` results (the LIMIT), `asc=false` means descending (newest first for BACKWARD direction). |
| `%12 = LOGQL_COMPAT %11` | **Compatibility wrapper** - Wraps the result to ensure compatibility with the LogQL output format. Handles any final column transformations needed for the V1 API. |
| `RETURN %12` | **Return result** - Designates `%12` as the final output of the plan. |

**Why this structure?**

1. **Predicates are created before use**: Stream selectors (`%1`, `%2`, `%3`) and filters (`%4`, `%6`, `%8`) are defined as standalone values before being used. This enables optimization passes to analyze and transform predicates independently.

2. **MAKETABLE centralizes data source definition**: All stream selection criteria go into one place, making it clear where data originates.

3. **Time filtering is explicit**: Rather than embedding time range in the data source, it's represented as explicit `SELECT` operations. This makes time range handling uniform and enables time-based optimizations.

4. **Line filters appear twice**: The predicate `%4` is both in MAKETABLE (for pushdown hints to storage) and in SELECT (for actual filtering). This redundancy allows the physical planner to optimize while ensuring correctness.

5. **TOPK handles both sorting and limiting**: For log queries, results need ordering by timestamp. TOPK combines sorting with limiting for efficiency.

### Example 2: Metric Query with Aggregation

**LogQL Query:**
```logql
sum by (level) (count_over_time({app="test"}[5m]))
```

**SSA Representation:**
```
%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = RANGE_AGGREGATION %6 [operation=count, start_ts=..., end_ts=..., step=0s, range=5m0s]
%8 = VECTOR_AGGREGATION %7 [operation=sum, group_by=(ambiguous.level)]
%9 = LOGQL_COMPAT %8
RETURN %9
```

**Line-by-Line Explanation:**

| Line | Instruction | Explanation |
|------|-------------|-------------|
| `%1 = EQ label.app "test"` | **Stream selector** - Simple equality predicate for streams where `app` label equals `"test"`. |
| `%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]` | **Data source** - Creates the table with stream selector. Note `predicates=[]` - metric queries typically don't have line filters (they count all lines). |
| `%3 = GTE builtin.timestamp ...` | **Start time predicate** - Note the time is 5 minutes *before* the query start time (`00:55:00` vs `01:00:00`). This "lookback" is needed because `count_over_time(...[5m])` needs 5 minutes of data before each evaluation point. |
| `%4 = SELECT %2 [predicate=%3]` | **Apply lookback start filter** - Includes extra data needed for the range aggregation lookback window. |
| `%5 = LT builtin.timestamp ...` | **End time predicate** - The actual query end time. |
| `%6 = SELECT %4 [predicate=%5]` | **Apply end time filter** - Now we have all data from (start - range) to end. |
| `%7 = RANGE_AGGREGATION %6 [operation=count, ...]` | **Range aggregation** - Aggregates data over time windows. `operation=count` for `count_over_time`, `range=5m0s` is the window size from `[5m]`. Produces one value per time window per series. |
| `%8 = VECTOR_AGGREGATION %7 [operation=sum, group_by=(ambiguous.level)]` | **Vector aggregation** - Aggregates across series. `operation=sum` for `sum(...)`, `group_by=(ambiguous.level)` means group by the `level` label. `ambiguous` type means the planner doesn't yet know if it's a stream label or parsed label. |
| `%9 = LOGQL_COMPAT %8` | **Compatibility wrapper** - Ensures metric result format compatibility. |
| `RETURN %9` | **Return result** - Final output. |

**Why `ambiguous` column type?**

The `ambiguous.level` in the group_by clause indicates that the planner doesn't know at logical planning time whether `level` is a:
- Stream label (`label.level`)
- Structured metadata (`metadata.level`)
- Parsed field (`parsed.level`)

This is resolved during physical planning when the actual schema is known. The physical planner will rewrite ambiguous references to their concrete types.

### Example 3: Log Query with JSON Parsing

**LogQL Query:**
```logql
{app="test"} | json | level="error"
```

**SSA Representation:**
```
%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp ...
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp ...
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*E, expr=PARSE_JSON(builtin.message, [], false, false)]
%8 = EQ ambiguous.level "error"
%9 = SELECT %7 [predicate=%8]
%10 = TOPK %9 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
%11 = LOGQL_COMPAT %10
RETURN %11
```

**Key Differences from Example 1:**

| Line | Explanation |
|------|-------------|
| `%7 = PROJECT %6 [mode=*E, expr=PARSE_JSON(...)]` | **JSON parsing projection** - The `| json` stage becomes a `PROJECT` node with `PARSE_JSON` expression. This extracts fields from the JSON log line and adds them as new columns. `mode=*E` indicates the projection mode. |
| `%8 = EQ ambiguous.level "error"` | **Filter on parsed field** - After JSON parsing, we can filter on extracted fields. `ambiguous.level` because we don't yet know if `level` came from stream labels or was parsed from JSON. |
| `%9 = SELECT %7 [predicate=%8]` | **Apply parsed field filter** - Filters rows where the parsed `level` field equals `"error"`. |

### Key Logical Nodes Reference

| Node | Purpose | LogQL Example |
|------|---------|---------------|
| `MakeTable` | Define data source with stream selector | `{app="test"}` |
| `Select` | Filter rows by predicate | `\| level="error"` |
| `Projection` | Transform/add columns (parsing, dropping) | `\| json`, `\| drop field` |
| `TopK` | Sort and limit (for log queries) | Implicit with LIMIT |
| `Limit` | Skip/fetch rows | `LIMIT 100` |
| `RangeAggregation` | Aggregate over time window | `count_over_time(...[5m])` |
| `VectorAggregation` | Aggregate across series | `sum by (level) (...)` |
| `BinOp` | Binary math operations | `... / 60` (for rate) |
| `LogqlCompat` | Output format compatibility wrapper | (always present) |

### Column Types in SSA

| Prefix | Meaning | Example |
|--------|---------|---------|
| `label.` | Stream label (from ingestion) | `label.app`, `label.namespace` |
| `metadata.` | Structured metadata | `metadata.traceID` |
| `parsed.` | Extracted during query (json, logfmt) | `parsed.level` |
| `builtin.` | Special engine columns | `builtin.timestamp`, `builtin.message` |
| `ambiguous.` | Type unknown until physical planning | `ambiguous.level` |

### Logical Planning Entry Point

```go
func BuildPlan(params logql.Params) (*Plan, error)
```

The planner examines `params.GetExpression()` to determine query type:
- `syntax.LogSelectorExpr`: Log query → Uses TopK for ordering by timestamp
- `syntax.SampleExpr`: Metric query → Uses Range/Vector aggregations

---

## Physical Planning

The physical planner (`pkg/engine/internal/planner/physical`) converts the logical SSA
representation into a Directed Acyclic Graph (DAG) of executable nodes.

### Key Differences from Logical Plan

1. **Data Sources**: Logical `MakeTable` becomes physical `DataObjScan` or `ScanSet` nodes
   pointing to actual data objects in storage.

2. **DAG Structure**: Nodes form a graph (not linear list) to support multiple inputs
   (e.g., binary operations between two queries).

3. **Optimization**: Physical plan supports optimization passes that modify the graph.

### Catalog System

The `Catalog` interface resolves logical stream selectors to physical data objects:

```go
type Catalog interface {
    ResolveShardDescriptors(selector Expression, from, through time.Time) ([]FilteredShardDescriptor, error)
    ResolveShardDescriptorsWithShard(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error)
}
```

`MetastoreCatalog` queries the metastore service to find data object locations.

### Physical Plan Example

```
LogQL: {app="test"} | level="error"

Physical Plan (tree representation):
Filter [predicate: EQ(level, "error")]
└── ColumnCompat
    └── ScanSet
        ├── DataObjScan [obj1, section 0]
        ├── DataObjScan [obj1, section 1]
        └── DataObjScan [obj2, section 0]
```

### Optimization Passes

The physical planner applies optimization passes in order:

1. **PredicatePushdown**: Push filters closer to data sources
2. **LimitPushdown**: Push LIMIT into scan nodes
3. **GroupByPushdown**: Push grouping into range aggregations
4. **ProjectionPushdown**: Push column selections into scans
5. **ParallelPushdown**: Insert `Parallelize` hints for distributed execution

---

## Workflow System

The workflow system (`pkg/engine/internal/workflow`) partitions a physical plan into
distributable tasks connected by data streams.

### Concepts

**Task**: A unit of work containing:
- `Fragment`: A portion of the physical plan to execute locally
- `Sources`: Input streams from other tasks
- `Sinks`: Output streams to other tasks or the final result
- `MaxTimeRange`: Time bounds for the data this task processes

**Stream**: A data channel between tasks:
- Has a unique ULID identifier
- Carries Arrow RecordBatches
- Has exactly one sender and one receiver

### Task Partitioning

The workflow planner identifies "pipeline breakers" - nodes that require seeing
all input data before producing output:

- `TopK`: Needs all data to determine top K rows
- `RangeAggregation`: Aggregates across time windows
- `VectorAggregation`: Aggregates across label groups

At each pipeline breaker, a new task boundary is created:

```
Physical Plan:
VectorAggregation
└── RangeAggregation
    └── Parallelize
        └── ScanSet [100 targets]

Workflow Tasks:
Task 1 (Root):
├── VectorAggregation
├── Sources: [Stream from Task 2]
└── Sinks: [Results Stream]

Task 2:
├── RangeAggregation
├── Sources: [Streams from Tasks 3-102]
└── Sinks: [Stream to Task 1]

Tasks 3-102 (Scan Tasks):
├── DataObjScan
├── Sources: []
└── Sinks: [Stream to Task 2]
```

### Admission Control

The workflow uses semaphore-based admission control:

```go
type Options struct {
    MaxRunningScanTasks  int  // Limit concurrent scan tasks
    MaxRunningOtherTasks int  // Limit concurrent non-scan tasks (0 = unlimited)
}
```

This prevents resource exhaustion when processing queries with thousands of scan nodes.

---

## Execution

The executor (`pkg/engine/internal/executor`) runs physical plan fragments and produces
Arrow RecordBatches.

### Pipeline Interface

```go
type Pipeline interface {
    Read(context.Context) (arrow.RecordBatch, error)
    Close()
}
```

Pipelines form a tree matching the physical plan structure. Data flows from leaves
(scan nodes) up to the root.

### Pipeline Types

| Type | Purpose |
|------|---------|
| `GenericPipeline` | Base pipeline with read function |
| `lazyPipeline` | Defers construction until first Read |
| `observedPipeline` | Wraps pipeline with statistics collection |
| `prefetchWrapper` | Async prefetching in background goroutine |

### Execution Flow

```go
func Run(ctx context.Context, cfg Config, plan *physical.Plan, logger log.Logger) Pipeline
```

The executor:
1. Walks the physical plan from root
2. Recursively creates pipelines for children
3. Constructs the appropriate pipeline type for each node
4. Returns a root pipeline that can be read for results

### Result Building

`ResultBuilder` converts Arrow RecordBatches to LogQL result types:

| Builder | Result Type | Query Type |
|---------|-------------|------------|
| `streamsResultBuilder` | `logqlmodel.Streams` | Log queries |
| `vectorResultBuilder` | `promql.Vector` | Instant metric queries |
| `matrixResultBuilder` | `promql.Matrix` | Range metric queries |

---

## Distributed Execution

For large queries, the engine distributes work across multiple workers.

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Scheduler                                │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    Task Queue                            │    │
│  │  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐           │    │
│  │  │ Task 1 │ │ Task 2 │ │ Task 3 │ │  ...   │           │    │
│  │  └────────┘ └────────┘ └────────┘ └────────┘           │    │
│  └─────────────────────────────────────────────────────────┘    │
│                              │                                   │
│                              ▼                                   │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │               Connected Workers                          │    │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │    │
│  │  │ Worker 1 │ │ Worker 2 │ │ Worker 3 │ │   ...    │   │    │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

### Scheduler

The scheduler (`pkg/engine/internal/scheduler`) coordinates task execution:

1. Receives task manifests from workflows
2. Assigns tasks to available workers
3. Manages stream bindings between tasks
4. Handles task status updates

### Worker

Workers (`pkg/engine/internal/worker`) execute tasks:

1. Connect to schedulers (local or remote)
2. Request tasks when ready
3. Execute task fragments using the executor
4. Send results via assigned streams

### Wire Protocol

Communication uses a binary protocol over:
- **Local**: In-process channels (`wire.Local`)
- **Remote**: HTTP/2 connections (`wire.HTTP2Listener/Dialer`)

Key message types:
- `WorkerHelloMessage`: Worker registration
- `TaskAssignMessage`: Scheduler assigns task to worker
- `TaskStatusMessage`: Worker reports task completion/failure
- `StreamBindMessage`: Bind streams between tasks
- `StreamDataMessage`: Carry Arrow RecordBatches

---

## Configuration

### Engine Configuration

```go
type Config struct {
    Enable      bool   // Enable V2 engine for supported queries
    Distributed bool   // Enable distributed execution
    
    Executor ExecutorConfig  // Execution settings
    Worker   WorkerConfig    // Worker settings
    
    StorageLag       time.Duration  // Time until data objects available (default: 1h)
    StorageStartDate flagext.Time   // Earliest valid data date
    
    EnableEngineRouter bool    // Route queries through V2 when in range
    DownstreamAddress  string  // Scheduler HTTP handler address
}
```

### Executor Configuration

```go
type ExecutorConfig struct {
    BatchSize          int         // Rows per Arrow batch (default: 100)
    MergePrefetchCount int         // Parallel inputs to prefetch in merge
    RangeConfig        rangeio.Config  // Object storage range read settings
}
```

### Worker Configuration

```go
type WorkerConfig struct {
    WorkerThreads          int           // Concurrent tasks per worker
    SchedulerLookupAddress string        // DNS for scheduler discovery
    SchedulerLookupInterval time.Duration // Scheduler refresh interval
}
```

---

## Storage System

The Query Engine V2 uses a new storage format called **DataObj** (Data Objects), which provides
columnar storage optimized for efficient log queries using Apache Arrow.

### DataObj Format

DataObj is a self-contained container format for object storage with the following structure:

```
┌────────────────────────────────────────────┐
│            Magic Header: "THOR"            │  4 bytes
├────────────────────────────────────────────┤
│                                            │
│            Section 0 Data                  │
│        (Logs: Parquet-encoded)             │
│                                            │
├────────────────────────────────────────────┤
│                                            │
│            Section 0 Streams               │
│         (Stream metadata)                  │
│                                            │
├────────────────────────────────────────────┤
│                                            │
│            Section 0 Index                 │
│           (Bloom filters)                  │
│                                            │
├────────────────────────────────────────────┤
│           ... more sections ...            │
├────────────────────────────────────────────┤
│                                            │
│            File Metadata                   │
│    (Section offsets, checksums, etc.)      │
│                                            │
├────────────────────────────────────────────┤
│             Footer (8 bytes)               │
│     [metadata_offset:4][magic:4]           │
└────────────────────────────────────────────┘
```

**Key Format Properties:**

| Property | Value |
|----------|-------|
| Magic Header | `"THOR"` (4 bytes) |
| Section Types | logs, streams, pointers (indexes) |
| Data Encoding | Parquet (columnar, compressed) |
| Index Type | Bloom filters for line content |
| File Suffix | `.dataobj` |

Each section contains:
- **Logs**: Log line data in Parquet columnar format
- **Streams**: Stream metadata (labels, timestamps)
- **Pointers**: Indexes (bloom filters) for query acceleration

### Storage Abstraction

The engine uses the **Thanos objstore.Bucket** interface for storage abstraction:

```go
import "github.com/thanos-io/objstore"

type Bucket interface {
    Upload(ctx context.Context, name string, r io.Reader) error
    Get(ctx context.Context, name string) (io.ReadCloser, error)
    GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error)
    Exists(ctx context.Context, name string) (bool, error)
    Delete(ctx context.Context, name string) error
    Iter(ctx context.Context, dir string, f func(string) error) error
    // ... more methods
}
```

**Supported Storage Backends:**

| Backend | Package | Use Case |
|---------|---------|----------|
| AWS S3 | `objstore/providers/s3` | Production cloud storage |
| Google Cloud Storage | `objstore/providers/gcs` | Production cloud storage |
| Azure Blob Storage | `objstore/providers/azure` | Production cloud storage |
| Alibaba OSS | `objstore/providers/oss` | Production cloud storage |
| **Filesystem** | `objstore/providers/filesystem` | **Local development/testing** |
| **In-Memory** | `objstore.NewInMemBucket()` | **Unit testing** |

**Local Storage is Fully Supported!** The filesystem and in-memory buckets provide
complete DataObj functionality without requiring cloud infrastructure.

### Write Path

The write path creates DataObj files from log streams:

```
Log Streams
    │
    ▼ (logsobj.Builder.Append)
Builder (in-memory)
    │
    ▼ (Builder.Flush)
dataobj.Object
    │
    ▼ (Uploader.Upload)
Object Storage (Bucket)
    │
    ▼ (TableOfContentsWriter.WriteEntry)
Metastore (TOC)
```

**Write Path Code Example:**

```go
import (
    "github.com/grafana/loki/v3/pkg/dataobj/logsobj"
    "github.com/grafana/loki/v3/pkg/dataobj/metastore"
    "github.com/thanos-io/objstore"
)

// 1. Create builder
builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
    TargetPageSize:   4 * 1024 * 1024,  // 4MB pages
    TargetObjectSize: 128 * 1024 * 1024, // 128MB objects
    BufferSize:       1 * 1024 * 1024,   // 1MB buffer
}, nil)

// 2. Create storage bucket (can be S3, filesystem, or in-memory)
bucket := objstore.NewInMemBucket()  // For testing
// bucket, _ := filesystem.NewBucket("/data/dataobj")  // For local disk

// 3. Create uploader
uploader := logsobj.NewUploader(bucket, "tenant-id")

// 4. Append log streams
for _, stream := range streams {
    for stream.Next() {
        record := stream.At()
        objects, err := builder.Append(tenant, record)
        // objects contains any flushed objects
    }
}

// 5. Flush remaining data
obj, closer, err := builder.Flush()
defer closer.Close()

// 6. Upload to storage
path, err := uploader.Upload(ctx, obj)

// 7. Write metadata to metastore
timeRanges := builder.TimeRanges()
tocWriter := metastore.NewTableOfContentsWriter(bucket, logger)
tocWriter.WriteEntry(ctx, path, timeRanges)
```

### Read Path

The read path queries data through the Catalog and Metastore:

```
Query (stream selector + time range)
    │
    ▼ (MetastoreCatalog.ResolveShardDescriptors)
Catalog
    │
    ▼ (Metastore.Sections)
Metastore
    │
    ▼ (returns FilteredShardDescriptor)
Section Descriptors (location, streamIDs, sections)
    │
    ▼ (dataobj.FromBucket)
DataObj Reader
    │
    ▼ (logs.Reader.Read)
Arrow RecordBatches
```

**Catalog Interface:**

The Catalog is the bridge between the physical planner and storage:

```go
// Catalog resolves stream selectors to physical data locations
type Catalog interface {
    // ResolveShardDescriptors returns data object locations matching the selector
    ResolveShardDescriptors(selector Expression, from, through time.Time) ([]FilteredShardDescriptor, error)
    
    // ResolveShardDescriptorsWithShard supports query sharding
    ResolveShardDescriptorsWithShard(selector Expression, predicates []Expression, 
        shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error)
}

// FilteredShardDescriptor identifies a scannable data section
type FilteredShardDescriptor struct {
    Location  DataObjLocation  // Object path: "tenant/dataobj-001"
    Streams   []int64          // Stream IDs matching selector
    Sections  []int            // Section indexes to scan
    TimeRange TimeRange        // Time range covered
}
```

**MetastoreCatalog Implementation:**

```go
// MetastoreCatalog wraps a Metastore for catalog operations
type MetastoreCatalog struct {
    ctx       context.Context
    metastore metastore.Metastore
}

func NewMetastoreCatalog(ctx context.Context, ms metastore.Metastore) *MetastoreCatalog {
    return &MetastoreCatalog{ctx: ctx, metastore: ms}
}

func (c *MetastoreCatalog) ResolveShardDescriptors(selector Expression, from, through time.Time) ([]FilteredShardDescriptor, error) {
    // 1. Convert selector expression to Prometheus matchers
    matchers, err := expressionToMatchers(selector, false)
    
    // 2. Query metastore for matching sections
    sections, err := c.metastore.Sections(c.ctx, from, through, matchers, nil)
    
    // 3. Convert to FilteredShardDescriptor
    return filterDescriptorsForShard(shard, sections)
}
```

**Metastore Interface:**

```go
// Metastore provides metadata queries for DataObj storage
type Metastore interface {
    // Sections returns data object sections matching the query
    Sections(ctx context.Context, start, end time.Time, 
        matchers, predicates []*labels.Matcher) ([]*DataobjSectionDescriptor, error)
    
    // Labels returns label names matching the query
    Labels(ctx context.Context, start, end time.Time, 
        matchers ...*labels.Matcher) ([]string, error)
    
    // Values returns label values for a given label name
    Values(ctx context.Context, start, end time.Time,
        matchers ...*labels.Matcher) ([]string, error)
}

// DataobjSectionDescriptor describes a single section
type DataobjSectionDescriptor struct {
    SectionKey SectionKey  // ObjectPath + SectionIdx
    StreamIDs  []int64     // Streams in this section
    Start      time.Time   // Section start time
    End        time.Time   // Section end time
}
```

**ObjectMetastore Implementation:**

```go
// ObjectMetastore reads metadata from object storage
type ObjectMetastore struct {
    bucket objstore.Bucket
    logger log.Logger
}

func NewObjectMetastore(bucket objstore.Bucket, logger log.Logger, reg prometheus.Registerer) *ObjectMetastore {
    return &ObjectMetastore{bucket: bucket, logger: logger}
}

func (m *ObjectMetastore) Sections(ctx context.Context, start, end time.Time,
    matchers, predicates []*labels.Matcher) ([]*DataobjSectionDescriptor, error) {
    // Reads TOC (table of contents) files from bucket
    // Returns sections matching time range and label matchers
}
```

### Reading DataObj Files

There are two methods to read DataObj files:

**1. From Object Storage (production):**

```go
import "github.com/grafana/loki/v3/pkg/dataobj"

// Read from bucket
obj, err := dataobj.FromBucket(ctx, bucket, "tenant/dataobj-001.dataobj")
defer obj.Close()

// Access sections
for i := 0; i < obj.NumSections(); i++ {
    section := obj.Section(i)
    // Read logs from section...
}
```

**2. From io.ReaderAt (testing/local):**

```go
import "github.com/grafana/loki/v3/pkg/dataobj"

// Read from file
file, _ := os.Open("/path/to/file.dataobj")
obj, err := dataobj.FromReaderAt(file, fileSize)
defer obj.Close()

// Or from bytes
reader := bytes.NewReader(data)
obj, err := dataobj.FromReaderAt(reader, int64(len(data)))
```

### Local Storage Setup for Testing

**Option 1: In-Memory Bucket (Unit Tests)**

```go
import (
    "github.com/thanos-io/objstore"
    "github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// Create in-memory bucket
bucket := objstore.NewInMemBucket()

// Create and use normally
builder, _ := logsobj.NewBuilder(config, nil)
uploader := logsobj.NewUploader(bucket, "test-tenant")

// Write data
builder.Append(tenant, stream)
obj, closer, _ := builder.Flush()
path, _ := uploader.Upload(ctx, obj)

// Write metadata
tocWriter := metastore.NewTableOfContentsWriter(bucket, logger)
tocWriter.WriteEntry(ctx, path, builder.TimeRanges())

// Create metastore and catalog
mstore := metastore.NewObjectMetastore(bucket, logger, nil)
catalog := physical.NewMetastoreCatalog(ctx, mstore)

// Use catalog in physical planner
planner := physical.NewPlanner(physical.NewContext(from, to), catalog)
```

**Option 2: Filesystem Bucket (Integration Tests)**

```go
import "github.com/thanos-io/objstore/providers/filesystem"

// Create filesystem bucket
bucket, err := filesystem.NewBucket("/tmp/loki-dataobj")

// Use exactly like S3 or any other bucket
// All DataObj operations work transparently
```

### Architecture Summary

```
                                  WRITE PATH
┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ Log Streams │───▶│   Builder   │───▶│   Object    │───▶│   Bucket    │
│             │    │ (logsobj)   │    │  (dataobj)  │    │  (objstore) │
└─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘
                                                               │
                                                               ▼
                                                        ┌─────────────┐
                                                        │ TOC Writer  │
                                                        │ (metastore) │
                                                        └─────────────┘

                                  READ PATH
┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Query     │───▶│   Catalog   │───▶│  Metastore  │───▶│   Bucket    │
│ (selector)  │    │ (physical)  │    │ (metastore) │    │  (objstore) │
└─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘
                         │                                      │
                         ▼                                      ▼
                  ┌─────────────┐                        ┌─────────────┐
                  │   Shard     │                        │   DataObj   │
                  │ Descriptors │                        │   Reader    │
                  └─────────────┘                        └─────────────┘
                         │                                      │
                         └──────────────────────────────────────┘
                                          │
                                          ▼
                                   ┌─────────────┐
                                   │ DataObjScan │
                                   │   (plan)    │
                                   └─────────────┘
```

### Mock Catalog for Testing

For unit tests where you don't need real storage, use a mock catalog:

```go
// mockCatalog implements physical.Catalog for testing
type mockCatalog struct {
    sectionDescriptors []*metastore.DataobjSectionDescriptor
}

func (c *mockCatalog) ResolveShardDescriptors(e physical.Expression, from, through time.Time) ([]physical.FilteredShardDescriptor, error) {
    // Return predefined descriptors
    var result []physical.FilteredShardDescriptor
    for _, desc := range c.sectionDescriptors {
        result = append(result, physical.FilteredShardDescriptor{
            Location:  physical.DataObjLocation(desc.ObjectPath),
            Streams:   desc.StreamIDs,
            Sections:  []int{int(desc.SectionIdx)},
            TimeRange: physical.TimeRange{Start: desc.Start, End: desc.End},
        })
    }
    return result, nil
}

// Usage in tests
catalog := &mockCatalog{
    sectionDescriptors: []*metastore.DataobjSectionDescriptor{
        {
            SectionKey: metastore.SectionKey{ObjectPath: "tenant/obj1", SectionIdx: 0},
            StreamIDs:  []int64{1, 2, 3},
            Start:      time.Now(),
            End:        time.Now().Add(time.Hour),
        },
    },
}
planner := physical.NewPlanner(physical.NewContext(from, to), catalog)
```

---

## Testing

The learning lab consists of comprehensive test files demonstrating each stage:

| File | Stage | Coverage |
|------|-------|----------|
| `stage1_logical_planning_test.go` | Logical Planning | SSA IR generation, instruction types |
| `stage2_physical_planning_test.go` | Physical Planning | DAG construction, optimization passes |
| `stage3_workflow_planning_test.go` | Workflow Planning | Task partitioning, stream bindings |
| `stage4_execution_test.go` | Execution | Pipeline creation, result building |
| `stage5_distributed_execution_test.go` | Distribution | Scheduler/worker interaction |
| `arrow_fundamentals_test.go` | Arrow | RecordBatch, schema, memory management |
| `end_to_end_test.go` | Integration | Complete query flow demonstration |

See `engine_learning_test.go` for an index of all learning tests.