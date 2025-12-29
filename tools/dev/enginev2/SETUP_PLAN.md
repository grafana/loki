# Engine V2 Development Environment - Setup Plan

## Overview

This plan details the complete setup for a local Engine V2 development environment with:
- **Write Path**: Docker Compose running Kafka + Loki components (Distributor + DataObj Consumer)
- **Read Path**: Local Loki process with Engine V2 enabled for querying
- **Storage**: Local filesystem for DataObj files (Parquet format)

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          WRITE PATH (Docker)                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Logs → Distributor → Kafka → DataObj Consumer → Filesystem     │
│         (port 3100)   (9092)   (port 3101)       (/loki/data)   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
                                ↓
                      Shared Filesystem Storage
                                ↓
┌─────────────────────────────────────────────────────────────────┐
│                     READ PATH (Local Process)                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Query → Querier (with Engine V2) → Filesystem → Query Results  │
│          (port 3200)                  (/loki/data)               │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Files to Create

### 1. `tools/dev/enginev2/docker-compose.yaml`

Docker Compose file with:
- **broker**: Apache Kafka (latest) with KRaft mode
  - Ports: 9092 (external), 29092 (internal)
  - Auto-create topics enabled
  - 16 partitions default
  
- **kafka-ui**: Kafka UI for monitoring
  - Port: 8080
  
- **loki-distributor**: Loki distributor with Kafka writes
  - Port: 3100 (HTTP API for log ingestion)
  - Port: 9095 (gRPC)
  - Target: `distributor`
  - Kafka writes enabled
  
- **loki-dataobj-consumer**: DataObj Consumer
  - Port: 3101 (HTTP for status/metrics)
  - Target: `dataobj-consumer`
  - Consumes from Kafka and writes DataObj files
  
- **grafana**: (Optional) Grafana for visualization
  - Port: 3000

**Volumes**:
- `kafka-data`: Kafka logs
- `loki-data`: Shared DataObj storage (mounted to `/loki` in containers)
- `grafana-data`: Grafana data

### 2. `tools/dev/enginev2/loki-write-config.yaml`

Loki configuration for write path components:

```yaml
auth_enabled: false
server:
  http_listen_port: 3100
  grpc_listen_port: 9095
  log_level: info

common:
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory
  replication_factor: 1
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules

# Memberlist for ring coordination
memberlist:
  node_name: loki-write
  join_members: []

# Schema configuration
schema_config:
  configs:
    - from: 2024-01-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

# Storage configuration
storage_config:
  filesystem:
    directory: /loki/chunks
  tsdb_shipper:
    active_index_directory: /loki/tsdb-index
    cache_location: /loki/tsdb-cache
  
  # Object store for DataObj
  object_store:
    backend: filesystem
    filesystem:
      dir: /loki/dataobj

# Kafka configuration
kafka_config:
  address: broker:29092
  topic: loki-logs

# Distributor configuration
distributor:
  ring:
    kvstore:
      store: inmemory
  
  # Enable Kafka writes
  kafka_writes_enabled: true
  ingester_writes_enabled: false

# DataObj configuration
dataobj:
  enabled: true
  storage_bucket_prefix: ""
  
  # Consumer configuration
  consumer:
    topic: loki-logs
    idle_flush_timeout: 60s
    
    # Builder configuration (embedded)
    target_page_size: 4194304      # 4MB
    target_object_size: 134217728  # 128MB
    buffer_size: 1048576           # 1MB
    
    # Lifecycler for ring membership
    lifecycler:
      ring:
        kvstore:
          store: inmemory
        replication_factor: 1
      
    # Partition ring configuration
    partition_ring:
      kvstore:
        store: inmemory
  
  # Metastore configuration
  metastore:
    index_storage_prefix: "index/v0"
    partition_ratio: 10

# Limits configuration
limits_config:
  ingestion_rate_mb: 100
  ingestion_burst_size_mb: 200
  max_streams_per_user: 0
  max_global_streams_per_user: 0
  reject_old_samples: false
```

### 3. `tools/dev/enginev2/loki-read-config.yaml`

Loki configuration for read path (local single-service):

```yaml
auth_enabled: false
server:
  http_listen_port: 3200
  grpc_listen_port: 9096
  log_level: info

common:
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory
  replication_factor: 1
  path_prefix: /absolute/path/to/loki/data  # UPDATE THIS PATH
  storage:
    filesystem:
      chunks_directory: /absolute/path/to/loki/data/chunks  # UPDATE THIS PATH
      rules_directory: /absolute/path/to/loki/data/rules    # UPDATE THIS PATH

# Schema configuration (must match write path)
schema_config:
  configs:
    - from: 2024-01-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

# Storage configuration (must match write path)
storage_config:
  filesystem:
    directory: /absolute/path/to/loki/data/chunks  # UPDATE THIS PATH
  tsdb_shipper:
    active_index_directory: /absolute/path/to/loki/data/tsdb-index  # UPDATE THIS PATH
    cache_location: /absolute/path/to/loki/data/tsdb-cache          # UPDATE THIS PATH
  
  # Object store for DataObj (must point to same location as write path)
  object_store:
    backend: filesystem
    filesystem:
      dir: /absolute/path/to/loki/data/dataobj  # UPDATE THIS PATH

# DataObj configuration (read-only)
dataobj:
  enabled: true
  storage_bucket_prefix: ""
  
  # Metastore configuration (must match write path)
  metastore:
    index_storage_prefix: "index/v0"
    partition_ratio: 10

# Query Engine V2 configuration
query_engine:
  enable: true
  distributed: false  # Single-service mode
  
  # Executor configuration
  executor:
    batch_size: 100
    merge_prefetch_count: 4
  
  # Storage lag - time until data objects are available
  storage_lag: 1h
  
  # Storage start date - earliest valid data date
  storage_start_date: "2024-01-01T00:00:00Z"
  
  # Enable engine router to direct queries to V2
  enable_engine_router: true

# Querier configuration
querier:
  engine:
    timeout: 5m
  max_concurrent: 4
  query_store_only: true  # Only query DataObj storage, not ingesters

# Query range configuration
query_range:
  align_queries_with_step: true
  cache_results: false  # Disable caching for development

# Limits configuration
limits_config:
  max_query_length: 721h  # 30 days
  max_query_lookback: 720h
  max_entries_limit_per_query: 10000
  max_query_series: 10000
```

### 4. `tools/dev/enginev2/README.md`

Comprehensive README with:
- Prerequisites
- Quick start instructions
- Step-by-step setup
- How to push logs
- How to query logs with Engine V2
- Troubleshooting guide
- Development workflow tips

## Configuration Details

### Key Kafka Settings

- **Topic**: `loki-logs` (auto-created by Kafka)
- **Partitions**: 16 (default)
- **Replication Factor**: 1 (single broker)
- **Bootstrap Servers**: `broker:29092` (internal), `localhost:9092` (external)

### Key DataObj Settings

- **Storage Format**: Parquet columnar format with bloom filters
- **Target Page Size**: 4MB (pages within sections)
- **Target Object Size**: 128MB (complete DataObj files)
- **Buffer Size**: 1MB (in-memory buffer)
- **Idle Flush Timeout**: 60s (flush after 60s of inactivity)

### Key Engine V2 Settings

- **Storage Lag**: 1h (time until data objects are available for querying)
- **Batch Size**: 100 rows per Arrow RecordBatch
- **Query Timeout**: 5m
- **Storage Start Date**: 2024-01-01 (earliest queryable date)

## Directory Structure

```
tools/dev/enginev2/
├── docker-compose.yaml          # Docker Compose for write path
├── loki-write-config.yaml       # Loki config for write components
├── loki-read-config.yaml        # Loki config for read component
├── README.md                    # User-facing documentation
├── SETUP_PLAN.md               # This file - implementation plan
└── data/                        # Created by Docker (gitignored)
    ├── kafka/                   # Kafka data
    ├── loki/                    # Loki DataObj storage
    │   ├── chunks/
    │   ├── dataobj/            # DataObj files (.dataobj)
    │   ├── tsdb-index/
    │   └── tsdb-cache/
    └── grafana/                 # Grafana data
```

## Implementation Steps

1. **Create directory structure**
   ```bash
   mkdir -p tools/dev/enginev2
   ```

2. **Create Docker Compose file** (`docker-compose.yaml`)
   - Configure Kafka with KRaft mode
   - Add Loki distributor service
   - Add DataObj consumer service
   - Add optional Kafka UI and Grafana

3. **Create write path config** (`loki-write-config.yaml`)
   - Enable Kafka writes in distributor
   - Configure DataObj consumer
   - Set filesystem storage paths
   - Configure rings for coordination

4. **Create read path config** (`loki-read-config.yaml`)
   - Enable Query Engine V2
   - Point to same filesystem storage
   - Configure single-service mode
   - Disable ingester querying

5. **Create README** (`README.md`)
   - Document prerequisites
   - Provide quick start guide
   - Explain how to push logs
   - Explain how to query with Engine V2
   - Add troubleshooting section

6. **Add `.gitignore`**
   ```
   tools/dev/enginev2/data/
   ```

## Testing the Setup

### 1. Start Write Path
```bash
cd tools/dev/enginev2
docker-compose up -d
```

### 2. Push Test Logs
```bash
curl -X POST http://localhost:3100/loki/api/v1/push \
  -H "Content-Type: application/json" \
  -d '{
    "streams": [
      {
        "stream": {
          "job": "test",
          "level": "info"
        },
        "values": [
          ["'$(date +%s)000000000'", "test log line 1"],
          ["'$(date +%s)000000001'", "test log line 2"]
        ]
      }
    ]
  }'
```

### 3. Verify DataObj Files Created
```bash
# Wait 60s for flush
sleep 60

# Check for DataObj files
find ./data/loki/dataobj -name "*.dataobj"
```

### 4. Start Read Path (Local)
```bash
# Update paths in loki-read-config.yaml first!
# Then run:
go run ./cmd/loki -target=querier -config.file=tools/dev/enginev2/loki-read-config.yaml
```

### 5. Query Logs with Engine V2
```bash
curl -G http://localhost:3200/loki/api/v1/query_range \
  --data-urlencode 'query={job="test"}' \
  --data-urlencode 'start='$(date -d '2 hours ago' +%s) \
  --data-urlencode 'end='$(date +%s)
```

## Next Steps for User

After creating these files, you should:

1. **Update paths** in `loki-read-config.yaml` to point to your Docker volume mount location
2. **Start the write path** with `docker-compose up -d`
3. **Push some test logs** using the curl commands
4. **Wait for DataObj flush** (60 seconds by default)
5. **Start the read path** locally with the Loki binary
6. **Query your logs** using Engine V2

## Development Workflow

For iterative Engine V2 development:

1. Keep write path running in Docker (stable)
2. Make changes to Engine V2 code in `pkg/engine/`
3. Rebuild Loki: `make loki`
4. Restart read path: `./cmd/loki/loki -target=querier -config.file=...`
5. Test queries immediately against existing DataObj files
6. No need to restart write path or re-ingest data

## Troubleshooting

Common issues and solutions to document in README:

- Kafka not starting: Check Docker resources
- No DataObj files: Wait for idle flush timeout (60s)
- Query returns no results: Check storage lag (1h default)
- Permission errors: Check filesystem permissions
- Out of memory: Reduce batch sizes or concurrent queries