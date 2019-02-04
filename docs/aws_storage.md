# AWS Storage setup instruction for Loki

## Example config via yaml file
```yaml
schema_config:
  configs:
  - from: 0
    store: aws
    object_store: aws
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

## S3 

Loki is using S3 as object storage. It stores log within directories based on
[`OrgID`](./operations.md#Multi-tenancy). For example, Logs from org `faker'
will stored in `s3://BUCKET_NAME/faker/`.

The S3 configuration is setup with url format: `s3://access_key:secret_access_key@region/bucket_name`.

## Dynamo DB

Loki uses dynamodb for the index storage. It is used for querying logs, make
sure you adjuest your throughput to your usage.

DynamoDB access is very similar to S3, however you do not need to specific a
table name in the storage section.  You will need to set the table name for
`index.prefix` inside schema config section.

To setup dynamodb, you can do it manually, or use `table_manager` to create and
maintain the tables for you.  You can find out more info about table manager at
[cortex
project](https://github.com/cortexproject/cortex)(https://github.com/cortexproject/cortex). There is an example table manager deployment inside the ksonnet deployment method.  You can find it [here](../production/ksonnet/loki/table-manager.libsonnet)
