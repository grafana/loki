# Stream Generator

A utility to generate streams (full or metadata) and send them to loki. This tool is useful for testing and benchmarking Loki's Kafka-based ingestion path.

## Building

From the root of the Loki repository:

```bash
docker build -t grafana/stream-generator -f tools/stream-generator/Dockerfile .
```

## Usage

The stream generator supports various configuration options through command-line flags:

### Basic Configuration

- `--tenants.total`: Number of tenants to generate streams or stream-metadata for (default: 1)
- `--tenants.streams.desired-rate`: Desired ingestion rate in bytes per second (default: 1MB/s)
- `--tenants.streams.total`: Number of streams per tenant (default: 100)
- `--tenants.qps`: Number of queries per second per tenant (default: 10)
- `--http-listen-port`: HTTP server listen address for metrics (default: ":9090")

### Kafka Configuration

- `--kafka.addresses`: The addresses of the Kafka brokers (comma separated) (default: "localhost:9092")
- `--kafka.topic`: The name of the Kafka topic to write to (default: "loki")
- `--kafka.client-id`: The client ID to use when connecting to Kafka (default: "stream-generator")
- `--kafka.version`: The version of the Kafka protocol to use (default: "2.3.0")
- `--kafka.timeout`: The timeout to use when connecting to Kafka (default: "10s")
- `--kafka.sasl.enabled`: Enable SASL authentication (default: false)
- `--kafka.sasl.mechanism`: SASL mechanism to use (default: "PLAIN")
- `--kafka.sasl.username`: SASL username for authentication (default: "")
- `--kafka.sasl.password`: SASL password for authentication (default: "")

### Ring Configuration

- `--ring.store`: The backend storage to use for the ring (supported: consul, etcd, inmemory, memberlist) (default: "inmemory")
- `--ring.replication-factor`: The number of replicas to write to (default: 3)
- `--ring.kvstore.store`: The backing store to use for the KVStore (default: "inmemory")
- `--ring.heartbeat-timeout`: The heartbeat timeout after which ingesters are considered unhealthy (default: "1m")

## Example Usage

### Basic Example

Run with default settings (1 tenant, 100 streams per tenant, 10 QPS):

```bash
docker run -p 9090:9090 grafana/stream-generator
```

### Full Example

Run with custom settings:

```bash
docker run -p 9091:9090 \
  grafana/stream-generator \
  --tenants.total=5 \
  --tenants.streams.total=1000 \
  --tenants.qps=100 \
  --http-listen-port=3100 \
  --kafka.address=kafka-1:9092 \
  --kafka.topic=loki \
  --kafka.client-id=stream-meta-gen-1 \
  --kafka.sasl.enabled=true \
  --kafka.sasl.mechanism=PLAIN \
  --kafka.sasl.username=loki \
  --kafka.sasl.password=secret123 \
  --stream-metadata-generator.store=consul \
  --stream-metadata-generator.replication-factor=3 \
  --stream-metadata-generator.kvstore.consul.hostname=consul:8500
```

### Local Development Example

For local development with the provided docker-compose setup:

```bash
docker run --network=host \
  grafana/stream-metadata-generator \
  --tenants.total=2 \
  --tenants.streams.total=500 \
  --tenants.qps=50 \
  --kafka.address=localhost:9092 \
  --kafka.topic=loki \
  --kafka.sasl.enabled=true \
  --kafka.sasl.mechanism=PLAIN \
  --kafka.sasl.username=loki \
  --kafka.sasl.password=secret123 \
  --stream-metadata-generator.store=inmemory
```

## Metrics

The generator exposes Prometheus metrics at `/metrics` on port 3100. When running with Docker, make sure to expose this port with the `-p` flag if you need to access the metrics from outside the container.

## Labels

The generator creates streams with the following labels:
- `cluster`
- `namespace`
- `job`
- `instance`

Each label value is generated using a pattern that ensures uniqueness across streams. 