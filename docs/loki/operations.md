# Operations

This page lists operational aspects of running Loki in alphabetical order:

## Authentication

Loki does not have an authentication layer.
You are expected to run an authenticating reverse proxy in front of your services, such as an Nginx with basic auth or an OAuth2 proxy.
See [client options](./promtail/deployment-methods.md#custom-client-options) for more details about supported authentication methods.

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
- `promtail_encoded_bytes_total` Number of bytes encoded and ready to send.
- `promtail_sent_bytes_total` Number of bytes sent.
- `promtail_dropped_bytes_total` Number of bytes dropped because failed to be sent to the ingester after all retries.
- `promtail_sent_entries_total` Number of log entries sent to the ingester.
- `promtail_dropped_entries_total` Number of log entries dropped because failed to be sent to the ingester after all retries.

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

Retention in Loki can be done by configuring Table Manager. You need to set a retention period and enable deletes for retention using yaml config as seen [here](https://github.com/grafana/loki/blob/39bbd733be4a0d430986d9513476a91334485e9f/production/ksonnet/loki/config.libsonnet#L128-L129) or using `table-manager.retention-period` and `table-manager.retention-deletes-enabled` command line args. Retention period needs to be a duration in string format that can be parsed using [time.Duration](https://golang.org/pkg/time/#ParseDuration).

**[NOTE]** Retention period should be at least twice the [duration of periodic table config](https://github.com/grafana/loki/blob/347a3e18f4976d799d51a26cee229efbc27ef6c9/production/helm/loki/values.yaml#L53), which currently defaults to 7 days.

In the case of chunks retention when using S3 or GCS, you need to set the expiry policy on the bucket that is configured for storing chunks. For more details check [this](https://docs.aws.amazon.com/AmazonS3/latest/dev/object-lifecycle-mgmt.html) for S3 and [this](https://cloud.google.com/storage/docs/managing-lifecycles) for GCS.

Currently we only support global retention policy. A per user retention policy and API to delete ingested logs is still under development.
Feel free to add your use case to this [GitHub issue](https://github.com/grafana/loki/issues/162).

A design goal of Loki is that storing logs should be cheap, hence a volume-based deletion API was deprioritized.

Until this feature is released: If you suddenly must delete ingested logs, you can delete old chunks in your object store.
Note that this will only delete the log content while keeping the label index intact.
You will still be able to see related labels, but the log retrieval of the deleted log content will no longer work.

## Scalability

See this [blog post](https://grafana.com/blog/2018/12/12/loki-prometheus-inspired-open-source-logging-for-cloud-natives/) on a discussion about Loki's scalability.

When scaling Loki, consider running several Loki processes with their respective roles of ingestor, distributor, and querier.
Take a look at their respective `.libsonnet` files in [our production setup](../production/ksonnet/loki) to get an idea about resource usage.

We're happy to get feedback about your resource usage.
