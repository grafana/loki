# Operations

This page lists operational aspects of running Loki in alphabetical order:

## Authentication

Loki does not have an authentication layer.
You are expected to run an authenticating reverse proxy in front of your services, such as an Nginx with basic auth or an OAuth2 proxy.

### Multi-tenancy

Loki is a multitenant system; requests and data for tenant A are isolated from tenant B.
Requests to the Loki API should include an HTTP header (`X-Scope-OrgID`) identifying the tenant for the request.
Tenant IDs can be any alphanumeric string; limiting them to 20 bytes is reasonable.

Loki can be run in "single-tenant" mode where the `X-Scope-OrgID` header is not required.
In this situation, the tenant ID is defaulted to be `fake`.

## Observability

### Metrics

Both Loki and promtail expose a `/metrics` endpoint for Prometheus metrics.

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

### Monitoring Mixins

Check out our [Loki mixin](../production/loki-mixin) for a set of dashboards, recording rules, and alerts.
These give you a comprehensive package on how to monitor Loki in production.

For more information about mixins, take a look at the [mixins project docs](https://github.com/monitoring-mixins/docs).

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

### Local storage

By default, Loki stores everything on disk.
The index is stored in a BoltDB under `/tmp/loki/index`.
The chunks are stored under `/tmp/loki/chunks`.

### Cloud storage

Loki has support for Google Cloud storage.
Take a look at our [production setup](https://github.com/grafana/loki/blob/a422f394bb4660c98f7d692e16c3cc28747b7abd/production/ksonnet/loki/config.libsonnet#L55) for the relevant configuration fields.

Support for AWS and Azure is also available, but not yet documented.
