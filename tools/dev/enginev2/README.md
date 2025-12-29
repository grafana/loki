# Loki Engine V2 Local Development Environment

This setup provides a complete local development environment for working on Loki's experimental Query Engine V2. It separates the write path (Docker) from the read path (local process) so you can iterate quickly on query engine changes without restarting ingestion.

## Architecture

**Docker services handle ingestion**, the **local querier reads from shared storage**.

The querier runs locally (in WSL or native Linux) and reads DataObj files directly from the shared filesystem. It does NOT need to join the Docker memberlist ring - it operates completely standalone.

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                         WRITE PATH (Docker Compose)                                  │
│                                                                                      │
│  ┌─────────┐    ┌─────────────┐    ┌───────┐    ┌──────────┐    ┌────────────────┐  │
│  │ Your App│───►│ Distributor │───►│ Kafka │───►│ Ingester │    │ DataObj        │  │
│  │ (curl)  │    │ (port 3100) │    │(9092) │    │ (3102)   │    │ Consumer (3101)│  │
│  └─────────┘    └──────┬──────┘    └───────┘    └──────────┘    │ (writes        │  │
│                        │                                        │  Parquet)      │  │
│                        │ DataObj Tee          ┌───────────────┐ └───────┬────────┘  │
│                        └─────────────────────►│ Kafka Topic   │─────────┘           │
│                                               │(loki-dataobjs)│         │           │
│                        ┌────────────────────┐ └───────────────┘         ▼           │
│                        │ Ingest Limits      │                  ┌────────────────┐   │
│                        │ Frontend (3103)    │                  │ DataObj Index  │   │
│                        └────────────────────┘                  │ Builder (3104) │   │
│                                                                └───────┬────────┘   │
└────────────────────────────────────────────────────────────────────────┼────────────┘
                                       │                                 │
                            Shared Filesystem Storage                    │
                            (./data/loki/dataobj)◄───────────────────────┘
                                       │           DataObj files + index/v0/
                                       ▼
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                         READ PATH (Local Go Process)                                 │
│                                                                                      │
│  ┌──────────────┐    ┌────────────────────────┐    ┌──────────────────┐             │
│  │ LogQL Query  │───►│ Querier (Engine V2)    │───►│ Query Results    │             │
│  │ (curl/logcli)│    │ (port 3200)            │    │                  │             │
│  └──────────────┘    └────────────────────────┘    └──────────────────┘             │
│                      Uses `inmemory` ring - completely standalone                    │
│                      Reads DataObj + index files directly from filesystem            │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

## What You Get

- **Kafka-based Ingestion**: Distributor writes to Kafka for buffering
- **DataObj Tee Pipeline**: Streams duplicated to separate topic for DataObj processing
- **DataObj Storage**: Consumer creates Parquet-based columnar storage
- **Ingest Limits Frontend**: Centralized rate tracking for the DataObj Tee
- **Engine V2**: Query engine using Apache Arrow for columnar processing
- **Local Filesystem**: All data stored locally (no cloud dependencies)
- **Fast Iteration**: Modify query engine code, rebuild, restart read path only
- **Separate Configs**: Docker services use `loki-config.yaml`, local querier uses `loki-querier-config.yaml`

## Prerequisites

- Docker and Docker Compose
- Go 1.25+ (for building Loki locally)
- 8GB+ RAM recommended
- Linux/macOS (or WSL2 on Windows)

## Quick Start

### 1. Start the Write Path

```bash
cd tools/dev/enginev2

# Build images from source (first time or after code changes)
docker-compose build

# Start services
docker-compose up -d
```

**Note**: The Docker Compose will build Loki from your local source code, so any changes you make to the distributor or consumer code will require rebuilding: `docker-compose build`

This starts:
- **Kafka** (port 9092) - Message broker
- **Kafka UI** (port 8080) - Monitor topics/messages
- **Loki Distributor** (port 3100) - Receives logs
- **Loki DataObj Consumer** (port 3101) - Writes Parquet files
- **Loki Ingester** (port 3102) - Partition ingester for Kafka mode
- **Loki Ingest Limits Frontend** (port 3103) - Rate tracking for DataObj Tee
- **Loki DataObj Index Builder** (port 3104) - Builds indexes for query discovery
- **Grafana** (port 3000) - Optional visualization

Wait for services to be ready (30-60 seconds):
```bash
docker-compose ps
```

All services should show "healthy" status.

### 2. Push Test Logs

```bash
# Simple test
curl -X POST http://localhost:3100/loki/api/v1/push \
  -H "Content-Type: application/json" \
  -d '{
    "streams": [{
      "stream": {"job": "test", "level": "info"},
      "values": [
        ["'$(date +%s)000000000'", "Engine V2 test log line 1"],
        ["'$(date +%s)000000001'", "Engine V2 test log line 2"],
        ["'$(date +%s)000000002'", "This is a metric.go log line"]
      ]
    }]
  }'
```

Push more logs with different labels:
```bash
curl -X POST http://localhost:3100/loki/api/v1/push \
  -H "Content-Type: application/json" \
  -d '{
    "streams": [{
      "stream": {"job": "app", "level": "error", "env": "dev"},
      "values": [
        ["'$(date +%s)000000000'", "Error: connection timeout"],
        ["'$(date +%s)000000001'", "Error: database unavailable"]
      ]
    }]
  }'
```

### 3. Wait for DataObj Flush

The DataObj consumer flushes data every 60 seconds by default. Wait a bit:

```bash
echo "Waiting 60 seconds for data flush..."
sleep 60
```

Verify DataObj files were created:
```bash
# Check DataObj files
ls -lh ./data/loki/dataobj/

# Or inside the container
docker exec loki-dataobj-consumer ls -lh /loki/dataobj/
```

You should see DataObj files created in the `objects/` subdirectory.

### 4. Start the Read Path (Local)

Run Loki locally. The querier uses a **separate config file** (`loki-querier-config.yaml`) that:
- Does NOT use memberlist (no network connection to Docker services needed)
- Uses `inmemory` ring for standalone operation
- Reads DataObj files directly from the shared filesystem

```bash
cd /path/to/loki  # Your repo root

go run cmd/loki/main.go \
  -target=querier \
  -config.file=tools/dev/enginev2/loki-querier-config.yaml
```

That's it! The querier starts on port 3200 and reads directly from `./tools/dev/enginev2/data/loki/dataobj/`.

**Note**: The querier config uses local paths and `query_store_only: true` to only read from storage, not from ingesters.

### 5. Query Your Logs

Using curl:
```bash
# Query logs from the last hour
curl -G http://localhost:3200/loki/api/v1/query_range \
  --data-urlencode 'query={job="test"}' \
  --data-urlencode "start=$(date -d '1 hour ago' +%s)" \
  --data-urlencode "end=$(date +%s)"
```

Using logcli:
```bash
# Install logcli if needed
go install ./cmd/logcli

# Query logs
logcli query '{job="test"}' \
  --addr=http://localhost:3200 \
  --from=1h \
  --limit=100
```

Test a metric query:
```bash
curl -G http://localhost:3200/loki/api/v1/query_range \
  --data-urlencode 'query=count_over_time({job="test"}[5m])' \
  --data-urlencode "start=$(date -d '1 hour ago' +%s)" \
  --data-urlencode "end=$(date +%s)" \
  --data-urlencode 'step=60s'
```

## Configuration

This setup uses **two configuration files**:

1. **`loki-config.yaml`** - Used by Docker services (distributor, consumer, ingester, etc.)
   - Uses memberlist with Docker DNS names for service discovery
   - Uses Docker internal Kafka address (`broker:29092`)
   
2. **`loki-querier-config.yaml`** - Used by the local querier
   - Uses `inmemory` ring (no memberlist - completely standalone)
   - Uses local filesystem paths
   - Has `query_store_only: true` to only read from DataObj storage

### Key Configuration Sections

| Section | Used By | Purpose |
|---------|---------|---------|
| `kafka_config` | Distributor, Ingester, DataObj Consumer | Kafka connection |
| `distributor` | Distributor | Kafka writes, DataObj Tee |
| `ingester` | Ingester | Partition ingester for Kafka mode |
| `ingest_limits` | Ingest Limits Service | Rate tracking state |
| `ingest_limits_frontend` | Ingest Limits Frontend | Rate tracking gateway |
| `dataobj` | DataObj Consumer | DataObj file creation |
| `query_engine` | Querier | Engine V2 settings |
| `querier` | Querier | Query execution |

### Key Settings

| Setting | Value | Purpose |
|---------|-------|---------|
| `kafka_config.topic` | `loki-logs` | Main ingestion topic |
| `distributor.dataobj_tee.topic` | `loki-dataobjs` | DataObj Tee output topic |
| `distributor.dataobj_tee.enabled` | `true` | Enable DataObj Tee pipeline |
| `ingest_limits.topic` | `loki-stream-metadata` | State replication topic |
| `dataobj.consumer.idle_flush_timeout` | `60s` | Flush interval |
| `query_engine.enable` | `true` | Enable Engine V2 |
| `query_engine.storage_lag` | `1h` | Data availability delay |
| `querier.query_store_only` | `true` | Only query DataObj storage |

## Development Workflow

This setup is optimized for Engine V2 development:

### 1. Make Code Changes

Edit files in `pkg/engine/`:
```bash
vim pkg/engine/internal/executor/pipeline.go
# Make your changes...
```

### 2. Rebuild Loki

```bash
make loki
```

### 3. Restart Read Path Only

Stop the local querier (Ctrl+C), then restart:
```bash
./cmd/loki/loki \
  -target=querier \
  -config.file=tools/dev/enginev2/loki-querier-config.yaml
```

**Note**: You do NOT need to restart the write path or re-ingest logs. Your DataObj files are persistent.

### 4. Test Immediately

Query against your existing DataObj files:
```bash
logcli query '{job="test"}' --addr=http://localhost:3200 --from=2h
```

### 5. Iterate

Repeat steps 1-4 as needed. The write path keeps running and accumulating data.

## Understanding the Setup

### Write Path Components

1. **Distributor**: Receives logs via HTTP/gRPC and writes to Kafka
   - Validates streams
   - Enforces rate limits
   - Shards data to Kafka partitions
   - **DataObj Tee**: Duplicates streams to `loki-dataobjs` topic

2. **Ingester**: Partition ingester consuming from Kafka
   - Required for distributor to find active partitions
   - Registers partitions in the partition ring

3. **Ingest Limits Frontend**: Gateway for rate tracking
   - Routes rate check requests to backend limits services
   - Required by DataObj Tee for rate tracking

4. **DataObj Consumer**: Consumes from Kafka and creates DataObj files
   - Reads from `loki-dataobjs` topic (DataObj Tee output)
   - Builds compressed Parquet files
   - Creates bloom filter indexes
   - Writes to object storage (filesystem)
   - Updates metastore for query discovery

### Read Path (Engine V2)

5. **Querier with Engine V2**: Executes LogQL queries using columnar processing
   - Parses LogQL queries
   - Creates logical plan (SSA IR)
   - Generates physical plan (DAG)
   - Executes using Arrow RecordBatches
   - Returns results to client

### Data Flow

```
1. Logs → HTTP/gRPC → Distributor
2. Distributor → Kafka Topic (loki-logs) → Ingester
3. Distributor → DataObj Tee → Kafka Topic (loki-dataobjs)
4. Kafka (loki-dataobjs) → DataObj Consumer
5. Consumer → Parquet Files (.dataobj format)
6. Consumer → Metastore (TOC entries)
7. Query → Engine V2 → Metastore (find DataObj files)
8. Engine V2 → Read DataObj → Process → Results
```

## Monitoring

### Kafka UI

Access Kafka UI at http://localhost:8080:
- View topics and partitions (`loki-logs`, `loki-dataobjs`, `loki-stream-metadata`)
- Monitor consumer lag
- Inspect messages

### Component Logs

```bash
# Distributor logs
docker-compose logs -f loki-distributor

# Consumer logs
docker-compose logs -f loki-dataobj-consumer

# Ingester logs
docker-compose logs -f loki-ingester

# Ingest Limits Frontend logs
docker-compose logs -f loki-ingest-limits-frontend

# Kafka logs
docker-compose logs -f broker
```

### Metrics

```bash
# Distributor metrics
curl http://localhost:3100/metrics

# Consumer metrics
curl http://localhost:3101/metrics

# Ingester metrics
curl http://localhost:3102/metrics

# Ingest Limits Frontend metrics
curl http://localhost:3103/metrics

# Querier metrics (local)
curl http://localhost:3200/metrics
```

### DataObj Files

Inspect DataObj files:
```bash
# List DataObj files
find ./data/loki/dataobj -name "*.dataobj" -exec ls -lh {} \;

# Use dataobj-inspect tool
go run ./cmd/dataobj-inspect stats ./data/loki/dataobj/objects/*.dataobj
```

## Troubleshooting

### No DataObj Files Created

**Problem**: After waiting 60s, no `.dataobj` files exist.

**Solutions**:
1. Check consumer logs: `docker-compose logs loki-dataobj-consumer`
2. Verify Kafka messages in `loki-dataobjs` topic: Check Kafka UI at http://localhost:8080
3. Check topic exists: `docker exec loki-kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list`
4. Verify DataObj Tee is enabled and working: Check distributor logs for "dataobj tee" messages

### Query Returns No Results

**Problem**: Queries return empty results even though logs were pushed.

**Solutions**:
1. **Storage Lag**: Default is 1h. Recent data won't be queryable. Push older logs or reduce `storage_lag`.
2. **Time Range**: Ensure your query time range includes the log timestamps.
3. **Labels**: Verify label selector matches your log streams: `curl http://localhost:3200/loki/api/v1/labels`
4. **DataObj Files**: Verify files exist and are readable in `./data/loki/dataobj/objects/`

### Kafka Connection Errors

**Problem**: Distributor can't connect to Kafka.

**Solutions**:
1. Wait for Kafka to be ready (can take 30-60s)
2. Check Kafka health: `docker-compose ps`
3. Verify network: `docker-compose exec loki-distributor ping broker`
4. Check Kafka logs: `docker-compose logs broker`

### Ingest Limits Frontend Errors

**Problem**: Distributor shows errors about ingest limits or rate tracking.

**Solutions**:
1. Ensure `loki-ingest-limits-frontend` is healthy: `docker-compose ps`
2. Check logs: `docker-compose logs loki-ingest-limits-frontend`
3. Verify memberlist connectivity - all services should join the same cluster

### Out of Memory

**Problem**: Services crash with OOM errors.

**Solutions**:
1. Reduce Docker memory limits in `docker-compose.yaml`
2. Reduce batch sizes in config
3. Reduce concurrent queries
4. Reduce DataObj target sizes

### Permission Denied on Filesystem

**Problem**: Can't write to `/loki` directories.

**Solutions**:
1. Check Docker volume permissions: `docker exec loki-dataobj-consumer ls -la /loki`
2. Ensure `./data/loki` directory exists with correct permissions: `mkdir -p ./data/loki && chmod -R 777 ./data`
3. Services run as user `10001:10001` - ensure the bind mount is writable

### Query Engine V2 Not Used

**Problem**: Queries still use Engine V1.

**Solutions**:
1. Verify `query_engine.enable: true` in config
2. Check query is supported by V2 (see `pkg/engine/validator.go`)
3. Verify logs show "using engine v2" message
4. Check time range is after `storage_start_date`

## Advanced Usage

### Running Distributed Execution

To test distributed query execution:

1. Enable distributed mode by adding flags:
   ```bash
   -query-engine.distributed=true
   ```

2. Start query scheduler and workers:
   ```bash
   # Terminal 1: Scheduler
   ./cmd/loki/loki -target=query-engine-scheduler -config.file=...
   
   # Terminal 2: Worker
   ./cmd/loki/loki -target=query-engine-worker -config.file=...
   
   # Terminal 3: Querier
   ./cmd/loki/loki -target=querier -config.file=...
   ```

### Custom Kafka Partitions

Edit `docker-compose.yaml`:
```yaml
environment:
  KAFKA_NUM_PARTITIONS: 32  # Increase parallelism
```

Then update consumer config for more partitions.

### Viewing Query Plans

Enable debug logging to see query plans:
```bash
./cmd/loki/loki ... -server.log-level=debug
```

Look for log lines containing:
- "logical plan" - SSA IR
- "physical plan" - DAG
- "workflow" - Task graph

## Cleaning Up

### Stop Services
```bash
docker-compose down
```

### Remove Data
```bash
docker-compose down -v  # Remove volumes too
rm -rf ./data/          # Remove bind-mounted data
```

### Rebuild from Scratch
```bash
docker-compose down -v
rm -rf ./data/
docker-compose build --no-cache
docker-compose up -d
```

### Rebuild After Code Changes

If you modify distributor or DataObj consumer code:
```bash
# Rebuild images from your source code
docker-compose build

# Restart services
docker-compose up -d
```

## Files

| File | Purpose |
|------|---------|
| `loki-config.yaml` | Configuration for Docker services (write path) |
| `loki-querier-config.yaml` | Configuration for local querier (read path) |
| `docker-compose.yaml` | Docker Compose definition for write path services |
| `data/loki/` | Bind-mounted storage directory (created on first run) |

## Next Steps

Now that you have a working environment:

1. **Explore the codebase**: Start with `pkg/engine/lab_test/ENGINE_V2_ARCHITECTURE.md`
2. **Read test files**: Check `pkg/engine/lab_test/*_test.go` for examples
3. **Make changes**: Modify executor, planner, or query validator
4. **Test thoroughly**: Use real queries on your local data
5. **Profile performance**: Use Go profiling tools with real workloads

## Getting Help

- **Architecture docs**: `pkg/engine/lab_test/ENGINE_V2_ARCHITECTURE.md`
- **Loki docs**: https://grafana.com/docs/loki/latest/
- **Issues**: https://github.com/grafana/loki/issues

Happy hacking! 🚀