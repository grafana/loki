# Overview of Loki

Grafana Loki is a set of components that can be composed into a fully featured
logging stack.

Unlike other logging systems, Loki is built around the idea of only indexing
labels for logs and leaving the original log message unindexed. This means
that Loki is cheaper to operate and can be orders of magnitude more efficient.

For a more detailed version of this same document, please read
[Architecture](../architecture.md).

## Multi Tenancy

Loki supports multi-tenancy so that data between tenants is completely
separated. Multi-tenancy is achieved through a tenant ID (which is represented
as an alphanumeric string). When multi-tenancy mode is disabled, all requests
are internally given a tenant ID of "fake".

## Modes of Operation

Loki is optimized for both running locally (or at small scale) and for scaling
horizontally: Loki comes with a _single process mode_ that runs all of the required
microservices in one process. The single process mode is great for testing Loki
or for running it at a small scale. For horizontal scalability, the
microservices of Loki can be broken out into separate processes, allowing them
to scale independently of each other.

## Components

### Distributor

The **distributor** service is responsible for handling logs written by
[clients](../clients/README.md). It's essentially the "first stop" in the write
path for log data. Once the distributor receives log data, it splits them into
batches and sends them to multiple [ingesters](#ingester) in parallel.

Distributors communicate with ingesters via [gRPC](https://grpc.io). They are
stateless and can be scaled up and down as needed.

#### Hashing

Distributors use consistent hashing in conjunction with a configurable
replication factor to determine which instances of the ingester service should
receive log data.

The hash is based on a combination of the log's labels and the tenant ID.

A hash ring stored in [Consul](https://www.consul.io) is used to achieve
consistent hashing; all [ingesters](#ingester) register themselves into the
hash ring with a set of tokens they own. Distributors then find the token that
most closely matches the value of the log's hash and will send data to that
token's owner.

#### Quorum consistency

Since all distributors share access to the same hash ring, write requests can be
sent to any distributor.

To ensure consistent query results, Loki uses
[Dynamo-style](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf)
quorum consistency on reads and writes. This means that the distributor will wait
for a positive response of at least one half plus one of the ingesters to send
the sample to before responding to the user.

### Ingester

The **ingester** service is responsible for writing log data to long-term
storage backends (DynamoDB, S3, Cassandra, etc.).

The ingester validates that ingested log lines are received in
timestamp-ascending order (i.e., each log has a timestamp that occurs at a later
time than the log before it). When the ingester receives a log that does not
follow this order, the log line is rejected and an error is returned.

Logs from each unique set of labels are built up into "chunks" in memory and
then flushed to the backing storage backend.

If an ingester process crashes or exits abruptly, all the data that has not yet
been flushed will be lost. Loki is usually configured to replicate multiple
replicas (usually 3) of each log to mitigate this risk.

#### Handoff

By default, when an ingester is shutting down and tries to leave the hash ring,
it will wait to see if a new ingester tries to enter before flushing and will
try to initiate a handoff. The handoff will transfer all of the tokens and
in-memory chunks owned by the leaving ingester to the new ingester.

This process is used to avoid flushing all chunks when shutting down, which is a
slow process.

#### Filesystem Support

While ingesters do support writing to the filesystem through BoltDB, this only
works in single-process mode as [queriers](#querier) need access to the same
back-end store and BoltDB only allows one process to have a lock on the DB at a
given time.

### Querier

The **querier** service handles the actual [LogQL](../logql.md) evaluation of
logs stored in long-term storage.

It first tries to query all ingesters for in-memory data before falling back to
loading data from the backend store.

## Chunk Store

The **chunk store** is Loki's long-term data store, designed to support
interactive querying and sustained writing without the need for background
maintenance tasks. It consists of:

* An index for the chunks. This index can be backed by
  [DynamoDB from Amazon Web Services](https://aws.amazon.com/dynamodb),
  [Bigtable from Google Cloud Platform](https://cloud.google.com/bigtable), or
  [Apache Cassandra](https://cassandra.apache.org).
* A key-value (KV) store for the chunk data itself, which can be DynamoDB,
  Bigtable, Cassandra again, or an object store such as
  [Amazon * S3](https://aws.amazon.com/s3)

> Unlike the other core components of Loki, the chunk store is not a separate
> service, job, or process, but rather a library embedded in the two services
> that need to access Loki data: the [ingester](#ingester) and [querier](#querier).

The chunk store relies on a unified interface to the
"[NoSQL](https://en.wikipedia.org/wiki/NoSQL)" stores (DynamoDB, Bigtable, and
Cassandra) that can be used to back the chunk store index. This interface
assumes that the index is a collection of entries keyed by:

* A **hash key**. This is required for *all* reads and writes.
* A **range key**. This is required for writes and can be omitted for reads,
which can be queried by prefix or range.

The interface works somewhat differently across the supported databases:

* DynamoDB supports range and hash keys natively. Index entries are thus
  modelled directly as DynamoDB entries, with the hash key as the distribution
  key and the range as the range key.
* For Bigtable and Cassandra, index entries are modelled as individual column
  values. The hash key becomes the row key and the range key becomes the column
  key.

A set of schemas are used to map the matchers and label sets used on reads and
writes to the chunk store into appropriate operations on the index. Schemas have
been added as Loki has evolved, mainly in an attempt to better load balance
writes and improve query performance.

> The current schema recommendation is the **v10 schema**.
