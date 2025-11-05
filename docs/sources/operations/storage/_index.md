---
title: Manage storage
menuTitle: Storage
description: Describes the Loki storage needs and supported stores.
---
# Manage storage

You can read a high level overview of Loki storage [here](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/)

Grafana Loki needs to store two different types of data: **chunks** and **indexes**.

When using Accelerated Search (experimental), then a third data type is used: **bloom blocks**.

Loki receives logs in separate streams, where each stream is uniquely identified
by its tenant ID and its set of labels. As log entries from a stream arrive,
they are compressed as **chunks** and saved in the chunks store. See [chunk
format](#chunk-format) for how chunks are stored internally.

The **index** stores each stream's label set and links them to the individual
chunks. Refer to the Loki [configuration](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/) for
details on how to configure the storage and the index.

For more information:

- [Table Manager](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/table-manager/)
- [Retention](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/retention/)
- [Logs Deletion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/logs-deletion/)

## Store Types

### ✅ Supported index stores

- [Single Store TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/) index store which stores TSDB index files in the object store. This is the recommended index store for Loki 2.8 and newer.
- [Single Store BoltDB (boltdb-shipper)](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/boltdb-shipper/) index store which stores boltdb index files in the object store. Recommended store for Loki 2.0 through 2.7.x.

### ❌ Deprecated index stores

- [Amazon DynamoDB](https://aws.amazon.com/dynamodb). Support for this is deprecated and will be removed in a future release.
- [Google Bigtable](https://cloud.google.com/bigtable). Support for this is deprecated and will be removed in a future release.
- [Apache Cassandra](https://cassandra.apache.org). Support for this is deprecated and will be removed in a future release.
- [BoltDB](https://github.com/boltdb/bolt) (doesn't work when clustering Loki)

### ✅ Supported and recommended chunks stores

- [Amazon Simple Storage Storage (S3)](https://aws.amazon.com/s3)
- [Google Cloud Storage (GCS)](https://cloud.google.com/storage/)
- [Microsoft Azure Blob Storage](https://azure.microsoft.com/en-us/products/storage/blobs)
- [IBM Cloud Object Storage (COS)](https://www.ibm.com/cloud/object-storage)
- [Baidu Object Storage (BOS)](https://cloud.baidu.com/product/bos.html)
- [Alibaba Object Storage Service (OSS)](https://www.alibabacloud.com/product/object-storage-service)

### ⚠️ Supported chunks stores, not typically recommended for production use

- [Filesystem](filesystem/) (please read more about the filesystem to understand the pros/cons before using with production data)
- S3 API compatible storage, such as [MinIO](https://min.io/)

### ❌ Deprecated chunks stores

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

See the [AWS deployment section](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#aws-deployment-s3-single-store) on the storage page for a detailed setup guide.

### DynamoDB

{{< admonition type="note" >}}
DynamoDB support is deprecated and will be removed in a future release.
{{< /admonition >}}

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

See the [IBM Cloud Object Storage section](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#ibm-deployment-cos-single-store) on the storage page for a detailed setup guide.

## Chunk Format

```
// Header
+-----------------------------------+
| Magic Number (uint32, 4 bytes)    |
+-----------------------------------+
| Version (1 byte)                  |
+-----------------------------------+
| Encoding (1 byte)                 |
+-----------------------------------+

// Blocks
+--------------------+----------------------------+
| block 1 (n bytes)  | checksum (uint32, 4 bytes) |
+--------------------+----------------------------+
| block 1 (n bytes)  | checksum (uint32, 4 bytes) |
+--------------------+----------------------------+
| ...                                             |
+--------------------+----------------------------+
| block N (n bytes)  | checksum (uint32, 4 bytes) |
+--------------------+----------------------------+

// Metas
+------------------------------------------------------------------------------------------------------------------------+
| #blocks (uvarint)                                                                                                      |
+--------------------+-----------------+-----------------+------------------+---------------+----------------------------+
| #entries (uvarint) | minTs (uvarint) | maxTs (uvarint) | offset (uvarint) | len (uvarint) | uncompressedSize (uvarint) |
+--------------------+-----------------+-----------------+------------------+---------------+----------------------------+
| #entries (uvarint) | minTs (uvarint) | maxTs (uvarint) | offset (uvarint) | len (uvarint) | uncompressedSize (uvarint) |
+--------------------+-----------------+-----------------+------------------+---------------+----------------------------+
| ...                                                                                                                    |
+--------------------+-----------------+-----------------+------------------+---------------+----------------------------+
| #entries (uvarint) | minTs (uvarint) | maxTs (uvarint) | offset (uvarint) | len (uvarint) | uncompressedSize (uvarint) |
+--------------------+-----------------+-----------------+------------------+---------------+----------------------------+
| checksum (uint32, 4 bytes)                                                                                             | 
+------------------------------------------------------------------------------------------------------------------------+

// Structured Metadata
+---------------------------------+
| #labels (uvarint)               |
+---------------+-----------------+
| len (uvarint) | value (n bytes) |
+---------------+-----------------+
| ...                             |
+---------------+-----------------+
| checksum (uint32, 4 bytes)      |
+---------------------------------+

// Footer
+-----------------------+--------------------------+
| len (uint64, 8 bytes) | offset (uint64, 8 bytes) |   // offset to Structured Metadata
+-----------------------+--------------------------+
| len (uint64, 8 bytes) | offset (uint64, 8 bytes) |   // offset to Metas
+-----------------------+--------------------------+
```
