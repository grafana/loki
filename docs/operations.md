# Operations

This page lists operational aspects of running Loki in alphabetical order:

## Authentication

Loki does not have an authentication layer.
You are expected to run an authenticating reverse proxy in front of your services, such as an Nginx with basic auth or an OAuth2 proxy.

### Multi-tenancy

Loki is a multitenant system; requests and data for tenant A are isolated from tenant B.
Requests to the Loki API should include an HTTP header (`X-Scope-OrgID`) identifying the tenant for the request.
Tenant IDs can be any alphanumeric string; limiting them to 20 bytes is reasonable.
To run in multitenant mode, loki should be started with `auth_enabled: true`.

Loki can be run in "single-tenant" mode where the `X-Scope-OrgID` header is not required.
In this situation, the tenant ID is defaulted to be `fake`.

## Observability

### Metrics

Both Loki and promtail expose a `/metrics` endpoint for Prometheus metrics.
You need a local Prometheus and make sure it can add your Loki and promtail as targets, [see Prometheus configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration).
When Prometheus can scrape Loki and promtail, you get the following metrics:

Loki metrics:

- `log_messages_total` Total number of log messages.
- `loki_distributor_bytes_received_total` The total number of uncompressed bytes received per tenant.
- `loki_distributor_lines_received_total` The total number of lines received per tenant.
- `loki_ingester_streams_created_total` The total number of streams created per tenant.
- `loki_request_duration_seconds_count` Number of received HTTP requests.

Promtail metrics:

- `promtail_read_bytes_total` Number of bytes read.
- `promtail_read_lines_total` Number of lines read.
- `promtail_request_duration_seconds_count` Number of send requests.
- `promtail_sent_bytes_total` Number of bytes sent.

Most of these metrics are counters and should continuously increase during normal operations:

1. Your app emits a log line to a file tracked by promtail.
2. Promtail reads the new line and increases its counters.
3. Promtail forwards the line to a Loki distributor, its received counters should increase.
4. The Loki distributor forwards it to a Loki ingester, its request duration counter increases.

You can import dashboard with ID [10004](https://grafana.com/dashboards/10004) to see them in Grafana UI.

### Monitoring Mixins

Check out our [Loki mixin](../production/loki-mixin) for a set of dashboards, recording rules, and alerts.
These give you a comprehensive package on how to monitor Loki in production.

For more information about mixins, take a look at the [mixins project docs](https://github.com/monitoring-mixins/docs).

## Retention/Deleting old data

A retention policy and API to delete ingested logs is still under development.
Feel free to add your use case to this [GitHub issue](https://github.com/grafana/loki/issues/162).

A design goal of Loki is that storing logs should be cheap, hence a volume-based deletion API was deprioritized.
But we realize that time-based retention could be a compliance issue.

Until this feature is released: If you suddenly must delete ingested logs, you can delete old chunks in your object store.
Note that this will only delete the log content while keeping the label index intact.
You will still be able to see related labels, but the log retrieval of the deleted log content will no longer work.

## Scalability

See this [blog post](https://grafana.com/blog/2018/12/12/loki-prometheus-inspired-open-source-logging-for-cloud-natives/) on a discussion about Loki's scalability.

When scaling Loki, consider running several Loki processes with their respective roles of ingestor, distributor, and querier.
Take a look at their respective `.libsonnet` files in [our production setup](../production/ksonnet/loki) to get an idea about resource usage.

We're happy to get feedback about your resource usage.

## Storage

Loki needs two stores: an index store and a chunk store.
Loki receives logs in separate streams.
Each stream is identified by a set of labels.
As the log entries from a stream arrive, they are gzipped as chunks and saved in the chunks store.
The index then stores the stream's label set, and links them to the chunks.
The chunk format refer to [doc](../pkg/chunkenc/README.md)

### Local storage

By default, Loki stores everything on disk.
The index is stored in a BoltDB under `/tmp/loki/index`.
The chunks are stored under `/tmp/loki/chunks`.

### Google Cloud Storage

Loki has support for Google Cloud storage.
Take a look at our [production setup](https://github.com/grafana/loki/blob/a422f394bb4660c98f7d692e16c3cc28747b7abd/production/ksonnet/loki/config.libsonnet#L55) for the relevant configuration fields.

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

You can also use an EC2 instance role instead of hard coding credentials like in the above example.
If you wish to do this the storage_config example looks like this:

```yaml
storage_config:
  aws:
    s3: s3://region/bucket_name
    dynamodbconfig:
      dynamodb: dynamodb://region
```


#### S3

Loki is using S3 as object storage. It stores log within directories based on
[`OrgID`](./operations.md#Multi-tenancy). For example, Logs from org `faker`
will stored in `s3://BUCKET_NAME/faker/`.

The S3 configuration is setup with url format: `s3://access_key:secret_access_key@region/bucket_name`.

#### DynamoDB

Loki uses DynamoDB for the index storage. It is used for querying logs, make
sure you adjuest your throughput to your usage.

DynamoDB access is very similar to S3, however you do not need to specify a
table name in the storage section, as Loki will calculate that for you.
You will need to set the table name prefix inside schema config section,
and ensure the `index.prefix` table exists.

You can setup DynamoDB by yourself, or have `table-manager` setup for you.
You can find out more info about table manager at
[Cortex project](https://github.com/cortexproject/cortex)(https://github.com/cortexproject/cortex).
There is an example table manager deployment inside the ksonnet deployment method. You can find it [here](../production/ksonnet/loki/table-manager.libsonnet)
The table-manager allows deleting old indices by rotating a number of different dynamodb tables and deleting the oldest one. If you choose to
create the table manually you cannot easily erase old data and your index just grows indefinitely.

If you set your DynamoDB table manually, ensure you set the primary index key to `h`
(string) and use `r` (binary) as the sort key. Also set the "period" attribute in the yaml to zero.
Make sure adjust your throughput base on your usage.

