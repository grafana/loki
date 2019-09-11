# Overview

Loki is the heart of the whole logging stack. It is responsible for permanently
storing the ingested log lines, as well as executing the queries against its
persistent store to analyze the contents.

## Architecture

Loki mainly consists of three and a half individual services that achieve this
in common. The high level architecture is based on Cortex, most of their documentation
usually applies to Loki as well.

### Distributor

The distributor can be considered the "first stop" for the log lines ingested by
the agents (e.g. Promtail).

It performs validation tasks on the data, splits it into batches and sends it to
multiple Ingesters in parallel.

Distributors communicate with ingesters via gRPC. They are *stateless* and can be scaled up and down as needed.

Refer to the [Cortex
docs](https://github.com/cortexproject/cortex/blob/master/docs/architecture.md#distributor)
for details on the internals.

### Ingester

The ingester service is responsible for de-duplicating and persisting the data
to long-term storage backends (DynamoDB, S3, Cassandra, etc.).

Ingesters are semi-*stateful*, the maintain the last 12 hours worth of logs
before flushing to the [Chunk store](#chunk-store). When restarting ingesters,
care must be taken no to lose this data.

More details can be found in the [Cortex
docs](https://github.com/cortexproject/cortex/blob/master/docs/architecture.md#ingester).

### Chunk store

Loki is not database, so it needs some place to persist the ingested log lines
to, for a longer period of time.

The chunk store is not really a service of Loki in the traditional way, but
rather some storage backend Loki uses.

It consists of a key-value (KV) store for the actual **chunk data** and an
**index store** to keep track of them. Refer to [Storage](storage.md) for details.

The [Cortex
docs](https://github.com/cortexproject/cortex/blob/master/docs/architecture.md#chunk-store)
also have good information about this.

### Querier

The Querier executes the LogQL queries from clients such as Grafana and LogCLI.

It fetches its data directly from the [Chunk store](#chunk-store) and the
[Ingesters](#ingester).

