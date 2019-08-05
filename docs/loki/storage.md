# Storage

Loki needs to store two different types of data: **Chunks** and **Indexes**.

Loki receives logs in separate streams. Each stream is identified by a set of labels.
As the log entries from a stream arrive, they are gzipped as chunks and saved in
the chunks store. The chunk format is documented in [`pkg/chunkenc`](../pkg/chunkenc/README.md).

On the other hand, the index stores the stream's label set and links them to the
individual chunks.

### Local storage

By default, Loki stores everything on disk. The index is stored in a BoltDB under
`/tmp/loki/index` and the chunks are stored under `/tmp/loki/chunks`.

### Google Cloud Storage

Loki supports Google Cloud Storage. Refer to Grafana Labs'
[production setup](https://github.com/grafana/loki/blob/a422f394bb4660c98f7d692e16c3cc28747b7abd/production/ksonnet/loki/config.libsonnet#L55)
for the relevant configuration fields.

### Cassandra

Loki can use Cassandra for the index storage. Example config using Cassandra:

```yaml
schema_config:
  configs:
  - from: 2018-04-15
    store: cassandra
    object_store: filesystem
    schema: v9
    index:
      prefix: cassandra_table
      period: 168h

storage_config:
  cassandra:
    username: cassandra
    password: cassandra
    addresses: 127.0.0.1
    auth: true
    keyspace: lokiindex

  filesystem:
    directory: /tmp/loki/chunks
```

### AWS S3 & DynamoDB

Example config for using S3 & DynamoDB:

```yaml
schema_config:
  configs:
    - from: 0
      store: dynamo
      object_store: s3
      schema: v9
      index:
        prefix: dynamodb_table_name
        period: 0
storage_config:
  aws:
    s3: s3://access_key:secret_access_key@region/bucket_name
    dynamodbconfig:
      dynamodb: dynamodb://access_key:secret_access_key@region
```

If you don't wish to hard-code S3 credentials, you can also configure an
EC2 instance role by changing the `storage_config` section:

```yaml
storage_config:
  aws:
    s3: s3://region/bucket_name
    dynamodbconfig:
      dynamodb: dynamodb://region
```

#### S3

Loki can use S3 as object storage, storing logs within directories based on
the [OrgID](./operations.md#Multi-tenancy). For example, logs from the `faker`
org will be stored in `s3://BUCKET_NAME/faker/`.

The S3 configuration is set up using the URL format:
`s3://access_key:secret_access_key@region/bucket_name`.

S3-compatible APIs (e.g., Ceph Object Storage with an S3-compatible API) can
be used. If the API supports path-style URL rather than virtual hosted bucket
addressing, configure the URL in `storage_config` with the custom endpoint:

```yaml
storage_config:
  aws:
    s3: s3://access_key:secret_access_key@custom_endpoint/bucket_name
    s3forcepathstyle: true
```

Loki needs the following permissions to write to an S3 bucket:

* s3:ListBucket
* s3:PutObject
* s3:GetObject

#### DynamoDB

Loki can use DynamoDB for storing the index. The index is used for querying
logs. Throughput to the index should be adjusted to your usage.

Access to DynamoDB is very similar to S3; however, a table name does not
need to be specified in the storage section, as Loki calculates that for
you. The table name prefix will need to be configured inside `schema_config`
for Loki to be able to create new tables.

DynamoDB can be set up manually or automatically through `table-manager`.
The `table-manager` allows deleting old indices by rotating a number of
different DynamoDB tables and deleting the oldest one. An example deployment
of the `table-manager` using ksonnet can be found
[here](../production/ksonnet/loki/table-manager.libsonnet) and more information
about it can be find at the
[Cortex project](https://github.com/cortexproject/cortex).

DynamoDB's `table-manager` client defaults provisioning capacity units
read to 300 and writes to 3000. The defaults can be overwritten in the
config:

```yaml
table_manager:
  index_tables_provisioning:
    provisioned_write_throughput: 10
    provisioned_read_throughput: 10
  chunk_tables_provisioning:
    provisioned_write_throughput: 10
    provisioned_read_throughput: 10
```

If DynamoDB is set up manually, old data cannot be easily erased and the index
will grow indefinitely. Manual configurations should ensure that the primary
index key is set to `h` (string) and the sort key is set to `r` (binary). The
"period" attribute in the yaml should be set to zero.

Loki needs the following permissions to write to DynamoDB:

* dynamodb:BatchGetItem
* dynamodb:BatchWriteItem
* dynamodb:DeleteItem
* dynamodb:DescribeTable
* dynamodb:GetItem
* dynamodb:ListTagsOfResource
* dynamodb:PutItem
* dynamodb:Query
* dynamodb:TagResource
* dynamodb:UntagResource
* dynamodb:UpdateItem
* dynamodb:UpdateTable
