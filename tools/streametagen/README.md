# Stream Metadata Generator

A utility to generate stream metadata for multiple tenants and push it into a Kafka topic. This tool is useful for testing and benchmarking Loki's Kafka-based ingestion path.

## Building

From the root of the Loki repository:

```bash
docker build -t grafana/stream-meta-gen -f tools/streametagen/Dockerfile .
```

## Usage

The stream metadata generator supports various configuration options through command-line flags:

### Basic Configuration

- `--tenants`: Number of tenants to generate metadata for (default: 1)
- `--streams`: Number of streams per tenant (default: 100)
- `--qps`: Number of queries per second per tenant (default: 10)
- `--http.listen-addr`: HTTP server listen address for metrics (default: ":9090")

### Kafka Configuration

- `--kafka.addresses`: The addresses of the Kafka brokers (comma separated) (default: "localhost:9092")
- `--kafka.topic`: The name of the Kafka topic to write to (default: "loki-ingest-metadata")
- `--kafka.client-id`: The client ID to use when connecting to Kafka (default: "stream-meta-gen")
- `--kafka.version`: The version of the Kafka protocol to use (default: "2.3.0")
- `--kafka.timeout`: The timeout to use when connecting to Kafka (default: "10s")
- `--kafka.sasl.enabled`: Enable SASL authentication (default: false)
- `--kafka.sasl.mechanism`: SASL mechanism to use (default: "PLAIN")
- `--kafka.sasl.username`: SASL username for authentication (default: "")
- `--kafka.sasl.password`: SASL password for authentication (default: "")

### Ring Configuration

- `--streammetagen_ring.store`: The backend storage to use for the ring (supported: consul, etcd, inmemory, memberlist) (default: "inmemory")
- `--streammetagen_ring.replication-factor`: The number of replicas to write to (default: 3)
- `--streammetagen_ring.kvstore.store`: The backing store to use for the KVStore (default: "inmemory")
- `--streammetagen_ring.heartbeat-timeout`: The heartbeat timeout after which ingesters are considered unhealthy (default: "1m")

## Example Usage

### Basic Example

Run with default settings (1 tenant, 100 streams per tenant, 10 QPS):

```bash
docker run -p 9090:9090 grafana/stream-meta-gen
```

### Full Example

Run with custom settings:

```bash
docker run -p 9091:9090 \
  grafana/stream-meta-gen \
  --tenants=5 \
  --streams=1000 \
  --qps=100 \
  --http.listen-addr=:9090 \
  --kafka.addresses=kafka-1:9092,kafka-2:9092 \
  --kafka.topic=loki-metadata \
  --kafka.client-id=stream-meta-gen-1 \
  --kafka.sasl.enabled=true \
  --kafka.sasl.mechanism=PLAIN \
  --kafka.sasl.username=loki \
  --kafka.sasl.password=secret123 \
  --streammetagen_ring.store=consul \
  --streammetagen_ring.replication-factor=3 \
  --streammetagen_ring.kvstore.consul.hostname=consul:8500
```

### Local Development Example

For local development with the provided docker-compose setup:

```bash
docker run --network=host \
  grafana/stream-meta-gen \
  --tenants=2 \
  --streams=500 \
  --qps=50 \
  --kafka.addresses=localhost:9092 \
  --kafka.topic=loki-ingest-metadata \
  --kafka.sasl.enabled=true \
  --kafka.sasl.mechanism=PLAIN \
  --kafka.sasl.username=loki \
  --kafka.sasl.password=secret123 \
  --streammetagen_ring.store=inmemory
```

## Metrics

The generator exposes Prometheus metrics at `/metrics` on port 9090. When running with Docker, make sure to expose this port with the `-p` flag if you need to access the metrics from outside the container.

## Labels

The generator creates streams with the following labels:
- `cluster`
- `namespace`
- `job`
- `instance`

Each label value is generated using a pattern that ensures uniqueness across streams. 