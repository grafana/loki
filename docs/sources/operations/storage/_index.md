---
title: Grafana Loki Storage
description: Grafana Loki Storage
weight: 40
---
# Grafana Loki Storage

[High level storage overview here]({{< relref "../../storage/_index.md" >}})

Grafana Loki needs to store two different types of data: **chunks** and **indexes**.

Loki receives logs in separate streams, where each stream is uniquely identified
by its tenant ID and its set of labels. As log entries from a stream arrive,
they are compressed as "chunks" and saved in the chunks store. See [chunk
format](#chunk-format) for how chunks are stored internally.

The **index** stores each stream's label set and links them to the individual
chunks.

Refer to Loki's [configuration]({{< relref "../../configure" >}}) for details on
how to configure the storage and the index.

For more information:

1. [Table Manager]({{< relref "./table-manager" >}})
1. [Retention]({{< relref "./retention" >}})
1. [Logs Deletion]({{< relref "./logs-deletion" >}})

## Supported Stores

The following are supported for the index:

- [TSDB]({{<relref "./tsdb" >}}) index store which stores TSDB index files in the object store. This is the recommended index store for Loki 2.8 and newer.
- [Single Store (boltdb-shipper)]({{<relref "./boltdb-shipper" >}}) index store which stores boltdb index files in the object store. 
- [Amazon DynamoDB](https://aws.amazon.com/dynamodb)
- [Google Bigtable](https://cloud.google.com/bigtable)
- [Apache Cassandra](https://cassandra.apache.org)
- [BoltDB](https://github.com/boltdb/bolt) (doesn't work when clustering Loki)

The following are deprecated for the index and will be removed in a future release:

- [Amazon DynamoDB](https://aws.amazon.com/dynamodb). Support for this is deprecated and will be removed in a future release.
- [Google Bigtable](https://cloud.google.com/bigtable). Support for this is deprecated and will be removed in a future release.
- [Apache Cassandra](https://cassandra.apache.org). Support for this is deprecated and will be removed in a future release.

The following are supported and recommended for the chunks:

- [Amazon S3](https://aws.amazon.com/s3)
- [Google Cloud Storage](https://cloud.google.com/storage/)
- [Filesystem]({{< relref "./filesystem" >}}) (please read more about the filesystem to understand the pros/cons before using with production data)
- [Baidu Object Storage](https://cloud.baidu.com/product/bos.html)
- [IBM Cloud Object Storage](https://www.ibm.com/cloud/object-storage)

The following are supported for the chunks, but not typically recommended for production use:

- [Filesystem]({{<relref "./filesystem"> }}) (please read more about the filesystem to understand the pros/cons before using with production data)

The following are deprecated for the chunks and will be removed in a future release:

- [Amazon DynamoDB](https://aws.amazon.com/dynamodb). Support for this is deprecated and will be removed in a future release.
- [Google Bigtable](https://cloud.google.com/bigtable). Support for this is deprecated and will be removed in a future release.
- [Apache Cassandra](https://cassandra.apache.org). Support for this is deprecated and will be removed in a future release.


## Cloud Storage Permissions

### S3

When using S3 as object storage, the following permissions are needed:

- `s3:ListBucket`
- `s3:PutObject`
- `s3:GetObject`
- `s3:DeleteObject` (if running the Single Store (boltdb-shipper) compactor)

Resources: `arn:aws:s3:::<bucket_name>`, `arn:aws:s3:::<bucket_name>/*`

See the [AWS deployment section]({{< relref "../../storage#aws-deployment-s3-single-store" >}}) on the storage page for a detailed setup guide.

### DynamoDB

{{% admonition type="note" %}}
DynamoDB support is deprecated and will be removed in a future release.
{{% /admonition %}}

When using DynamoDB for the index, the following permissions are needed:

- `dynamodb:BatchGetItem`
- `dynamodb:BatchWriteItem`
- `dynamodb:DeleteItem`
- `dynamodb:DescribeTable`
- `dynamodb:GetItem`
- `dynamodb:ListTagsOfResource`
- `dynamodb:PutItem`
- `dynamodb:Query`
- `dynamodb:TagResource`
- `dynamodb:UntagResource`
- `dynamodb:UpdateItem`
- `dynamodb:UpdateTable`
- `dynamodb:CreateTable`
- `dynamodb:DeleteTable` (if `table_manager.retention_period` is more than 0s)

Resources: `arn:aws:dynamodb:<aws_region>:<aws_account_id>:table/<prefix>*`

- `dynamodb:ListTables`

Resources: `*`

#### AutoScaling

If you enable autoscaling from table manager, the following permissions are needed:

##### Application Autoscaling

- `application-autoscaling:DescribeScalableTargets`
- `application-autoscaling:DescribeScalingPolicies`
- `application-autoscaling:RegisterScalableTarget`
- `application-autoscaling:DeregisterScalableTarget`
- `application-autoscaling:PutScalingPolicy`
- `application-autoscaling:DeleteScalingPolicy`

Resources: `*`

##### IAM

- `iam:GetRole`
- `iam:PassRole`

Resources: `arn:aws:iam::<aws_account_id>:role/<role_name>`


### IBM Cloud Object Storage

When using IBM Cloud Object Storage (COS) as object storage, IAM `Writer` role is needed.

See the [IBM Cloud Object Storage section]({{< relref "../../storage#ibm-cloud-object-storage" >}}) on the storage page for a detailed setup guide.

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

