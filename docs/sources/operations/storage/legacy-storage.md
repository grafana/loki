---
title: Legacy storage
menuTitle:
description: Describes deprecated legacy storage options for Loki that are superseded by single store.
weight: 1000
---
# Legacy storage

{{< admonition type="warning" >}}
The concepts described on this page are considered legacy and pre-date the single store storage introduced in Loki 2.0.
The usage of legacy storage for new installations is highly discouraged and documentation is meant for informational
purposes in case of upgrade to a single store.
{{< /admonition >}}

The **chunk store** is the Loki long-term data store, designed to support
interactive querying and sustained writing without the need for background
maintenance tasks. It consists of:

- An index for the chunks. This index can be backed by:
    - [Amazon DynamoDB](https://aws.amazon.com/dynamodb)
    - [Google Bigtable](https://cloud.google.com/bigtable)
    - [Apache Cassandra](https://cassandra.apache.org)
- A key-value (KV) store for the chunk data itself, which can be:
    - [Amazon DynamoDB](https://aws.amazon.com/dynamodb)
    - [Google Bigtable](https://cloud.google.com/bigtable)
    - [Apache Cassandra](https://cassandra.apache.org)
    - [Amazon S3](https://aws.amazon.com/s3)
    - [Google Cloud Storage](https://cloud.google.com/storage/)

{{< admonition type="note" >}}
Unlike the other core components of Loki, the chunk store is not a separate
service, job, or process, but rather a library embedded in the two services
that need to access Loki data: the [ingester](../../../get-started/components/#ingester) and [querier](../../../get-started/components/#querier).
{{< /admonition >}}

The chunk store relies on a unified interface to the
"[NoSQL](https://en.wikipedia.org/wiki/NoSQL)" stores (DynamoDB, Bigtable, and
Cassandra) that can be used to back the chunk store index. This interface
assumes that the index is a collection of entries keyed by:

- A **hash key**. This is required for *all* reads and writes.
- A **range key**. This is required for writes and can be omitted for reads,
which can be queried by prefix or range.

The interface works somewhat differently across the supported databases:

- DynamoDB supports range and hash keys natively. Index entries are thus
  modelled directly as DynamoDB entries, with the hash key as the distribution
  key and the range as the DynamoDB range key.
- For Bigtable and Cassandra, index entries are modelled as individual column
  values. The hash key becomes the row key and the range key becomes the column
  key.

A set of schemas are used to map the matchers and label sets used on reads and
writes to the chunk store into appropriate operations on the index. Schemas have
been added as Loki has evolved, mainly in an attempt to better load balance
writes and improve query performance.
