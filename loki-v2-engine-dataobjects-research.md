# Loki V2 Query Engine and Data Objects - Research Summary

## Overview

The new Loki query engine (v2) is a complete reimplementation that reads from **data objects** instead of traditional chunks. Data objects use a columnar storage format that provides better compression and query performance for large-scale deployments.

## Architecture Components

### 1. Query Execution Pipeline (`/workspace/pkg/engine/engine.go`)

The v2 engine executes queries in three phases:

1. **Logical Planning** (`/workspace/pkg/engine/planner/logical/planner.go`)
   - Converts LogQL AST into a logical query plan
   - Currently supports:
     - Log queries (backward direction only)
     - Instant metric queries
     - Basic filters and label matchers
   - Not yet supported:
     - Forward log queries
     - Range metric queries  
     - Parser expressions (logfmt, json, etc.)
     - Line/label formatting

2. **Physical Planning** (`/workspace/pkg/engine/planner/physical/planner.go`)
   - Converts logical plan to physical execution plan
   - Uses metastore catalog to resolve data object locations
   - Creates `DataObjScan` operations that specify:
     - Object location in S3
     - Stream IDs to read
     - Predicates to filter
     - Projections for columns

3. **Execution** (`/workspace/pkg/engine/executor/`)
   - Executes the physical plan using Arrow-based pipelines
   - Reads columnar data from data objects
   - Applies predicates and projections
   - Returns results as Arrow records

### 2. Data Object Structure

Data objects are stored in S3 with columnar format:

- **Logs Section** (`/workspace/pkg/dataobj/sections/logs/`)
  - Columnar storage for log entries
  - Sorted by timestamp
  - Supports predicate pushdown
  
- **Streams Section** (`/workspace/pkg/dataobj/sections/streams/`)
  - Contains stream metadata and labels
  - Maps stream IDs to label sets

- **Metastore** (`/workspace/pkg/dataobj/metastore/`)
  - Tracks data object locations
  - Provides stream ID resolution
  - Supports both direct and index-based lookups

### 3. Query Path Selection (`/workspace/pkg/querier/http.go:90-110`)

The v2 engine is used when:
- `--querier.engine.enable-v2-engine=true` flag is set
- Query ends > 1 hour ago (to ensure data objects are available)
- Query is supported by the new engine

Falls back to v1 engine if:
- Query uses unsupported features
- Returns `engine.ErrNotSupported`

### 4. Data Flow with Kafka

When Kafka ingestion is enabled:

```
Logs → Distributor → Kafka → Ingester (Kafka Consumer) → Data Objects → S3
                                        ↓
                              DataObj Index Builder → Indexes → S3
```

Key components:
- **Distributor**: Writes logs to Kafka topics (when `kafka_writes.enabled: true`)
- **Ingester with Kafka**: Consumes from Kafka (when `kafka_ingestion.enabled: true`)
- **Partition Ring**: Coordinates partition assignments between consumers
- **Data Object Consumer**: Builds columnar data objects and uploads to S3
- **Index Builder**: Creates indexes for efficient querying

## Configuration Requirements

### Loki Config (`loki.yaml`)

```yaml
# Enable Kafka
kafka_config:
    kafka_target: kafka:29092
    kafka_topic: loki

# Data object settings
dataobj:
    storage_bucket_prefix: dataobj/
    consumer:
        idle_flush_timeout: 1h
        uploader:
            flush_interval: 5m
    metastore:
        storage_config:
            backend: filesystem
            filesystem:
                directory: /data/metastore

# Distributor with Kafka writes
distributor:
    kafka_writes:
        enabled: true
        kafka_config:
            kafka_target: kafka:29092
            kafka_topic_template: "loki_{{.TenantID}}"

# Ingester with Kafka consumption  
ingester:
    kafka_ingestion:
        enabled: true

# Querier with v2 engine
querier:
    engine:
        enable_v2_engine: true
        batch_size: 512
        dataobjscan_page_cache_size: 10MB
```

### Required Services

1. **partition-ring** - Coordination service for Kafka consumers
2. **dataobj-consumer** - Consumes from Kafka and writes data objects
3. **dataobj-index-builder** - Builds indexes for data objects
4. **querier** with v2 engine flag enabled

## Key Files for Understanding the Implementation

### Core Engine
- `/workspace/pkg/engine/engine.go` - Main QueryEngine implementation
- `/workspace/pkg/engine/planner/logical/planner.go` - Logical planning
- `/workspace/pkg/engine/planner/physical/planner.go` - Physical planning
- `/workspace/pkg/engine/planner/physical/dataobjscan.go` - DataObjScan operation
- `/workspace/pkg/engine/planner/physical/catalog.go` - Metastore catalog
- `/workspace/pkg/engine/executor/dataobjscan.go` - Data object scanning
- `/workspace/pkg/engine/executor/executor.go` - Pipeline executor

### Data Objects
- `/workspace/pkg/dataobj/sections/logs/` - Log section format
- `/workspace/pkg/dataobj/sections/streams/` - Stream section format
- `/workspace/pkg/dataobj/metastore/` - Metadata storage
- `/workspace/pkg/dataobj/consumer/` - Kafka consumer implementation

### Integration
- `/workspace/pkg/querier/http.go` - Query path selection logic
- `/workspace/pkg/loki/modules.go` - Module initialization
- `/workspace/pkg/logql/engine.go` - Engine configuration

## Debugging the V2 Query Path

### Breakpoint Locations
1. `/workspace/pkg/querier/http.go:90` - Query path selection
2. `/workspace/pkg/engine/engine.go:81` - Main Execute method
3. `/workspace/pkg/engine/planner/logical/planner.go:22` - Logical planning
4. `/workspace/pkg/engine/planner/physical/planner.go:144` - Physical planning
5. `/workspace/pkg/engine/executor/dataobjscan.go:68` - Data object reading
6. `/workspace/pkg/dataobj/sections/logs/reader.go` - Columnar data reading

### Sample Query for Testing

```bash
# Query must end > 1 hour ago for v2 engine activation
logcli query '{job="test"} | line_contains "error"' \
  --addr=http://localhost:3100 \
  --from="2h" \
  --to="61m" \
  --limit=100
```

### Verification

Look for these log messages:
- "executing with new engine" - v2 engine is being used
- "falling back to legacy query engine" - v2 engine unsupported query
- Query response metadata warning: "Query was executed using the new experimental query engine and dataobj storage."

## Current Limitations

1. **Query Support**
   - No forward log queries
   - No range metric queries
   - No parser expressions (logfmt, json, etc.)
   - No line/label formatting

2. **Time Constraint**
   - Only queries ending > 1 hour ago use v2 engine
   - This ensures data objects are available in S3

3. **Storage**
   - Requires Kafka for ingestion
   - Requires S3-compatible storage for data objects
   - Metastore currently uses filesystem backend

## Thor Environment Setup

The thor environment has been configured with:
- Kafka for log streaming
- Partition ring for coordination
- Ingesters with Kafka ingestion enabled (using `-ingester.kafka-ingestion-enabled=true` flag)
- Querier with v2 engine support (needs `-querier.engine.enable-v2-engine=true` flag)
- MinIO for S3-compatible storage

Note: The current setup uses `ingester` target with Kafka flags instead of separate `dataobj-consumer` and `dataobj-index-builder` targets, which may be a transitional approach.