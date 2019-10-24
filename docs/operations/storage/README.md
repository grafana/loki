# Loki Storage

Loki needs to store two different types of data: **chunks** and **indexes**.

Loki receives logs in separate streams, where each stream is uniquely identified
by its tenant ID and its set of labels. As log entries from a stream arrive,
they are GZipped as "chunks" and saved in the chunks store. See [chunk
format](#chunk-format) for how chunks are stored internally.

The **index** stores each stream's label set and links them to the individual
chunks.

Refer to Loki's [configuration](../../configuration/README.md) for details on
how to configure the storage and the index.

For more information:

1. [Table Manager](table-manager.md)
2. [Retention](retention.md)

## Supported Stores

The following are supported for the index:

* [Amazon DynamoDB](https://aws.amazon.com/dynamodb)
* [Google Bigtable](https://cloud.google.com/bigtable)
* [Apache Cassandra](https://cassandra.apache.org)
* [BoltDB](https://github.com/boltdb/bolt) (doesn't work when clustering Loki)

The following are supported for the chunks:

* [Amazon DynamoDB](https://aws.amazon.com/dynamodb)
* [Google Bigtable](https://cloud.google.com/bigtable)
* [Apache Cassandra](https://cassandra.apache.org)
* [Amazon S3](https://aws.amazon.com/s3)
* [Google Cloud Storage](https://cloud.google.com/storage/)
* Filesystem (doesn't work when clustering Loki)

## Cloud Storage Permissions

### S3

When using S3 as object storage, the following permissions are needed:

* `s3:ListBucket`
* `s3:PutObject`
* `s3:GetObject`

### DynamoDB

When using DynamoDB for the index, the following permissions are needed:

* `dynamodb:BatchGetItem`
* `dynamodb:BatchWriteItem`
* `dynamodb:DeleteItem`
* `dynamodb:DescribeTable`
* `dynamodb:GetItem`
* `dynamodb:ListTagsOfResource`
* `dynamodb:PutItem`
* `dynamodb:Query`
* `dynamodb:TagResource`
* `dynamodb:UntagResource`
* `dynamodb:UpdateItem`
* `dynamodb:UpdateTable`

## Chunk Format

```
  -------------------------------------------------------------------
  |                               |                                 |
  |        MagicNumber(4b)        |           version(1b)           |
  |                               |                                 |
  -------------------------------------------------------------------
  |         block-1 bytes         |          checksum (4b)          |
  -------------------------------------------------------------------
  |         block-2 bytes         |          checksum (4b)          |
  -------------------------------------------------------------------
  |         block-n bytes         |          checksum (4b)          |
  -------------------------------------------------------------------
  |                        #blocks (uvarint)                        |
  -------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  |                      checksum(from #blocks)                     |
  -------------------------------------------------------------------
  |           metasOffset - offset to the point with #blocks        |
  -------------------------------------------------------------------
```

