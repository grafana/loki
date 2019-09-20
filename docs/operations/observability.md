# Observing Loki

Both Loki and promtail expose a `/metrics` endpoint that expose Prometheus
metrics. You will need a local Prometheus and add Loki and promtail as targets.
See [configuring
Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/configuration)
for more information.

Loki exposes the following metrics:

- `log_messages_total`: Total number of log messages.
- `loki_distributor_bytes_received_total`: The total number of uncompressed bytes received per tenant.
- `loki_distributor_lines_received_total`: The total number of lines received per tenant.
- `loki_ingester_streams_created_total`: The total number of streams created per tenant.
- `loki_request_duration_seconds_count`: Number of received HTTP requests.

Promtail exposes these metrics:

- `promtail_read_bytes_total`: Number of bytes read.
- `promtail_read_lines_total`: Number of lines read.
- `promtail_request_duration_seconds_count`: Number of send requests.
- `promtail_encoded_bytes_total`: Number of bytes encoded and ready to send.
- `promtail_sent_bytes_total`: Number of bytes sent.
- `promtail_dropped_bytes_total`: Number of bytes dropped because failed to be sent to the ingester after all retries.
- `promtail_sent_entries_total`: Number of log entries sent to the ingester.
- `promtail_dropped_entries_total`: Number of log entries dropped because failed to be sent to the ingester after all retries.

Most of these metrics are counters and should continuously increase during normal operations:

1. Your app emits a log line to a file that is tracked by promtail.
2. Promtail reads the new line and increases its counters.
3. Promtail forwards the log line to a Loki distributor, where the received
   counters should increase.
4. The Loki distributor forwards the log line to a Loki ingester, where the
   request duration counter should increase.

If Promtail uses any pipelines with metrics stages, those metrics will also be
exposed by Promtail at its `/metrics` endpoint. See Promtail's documentation on
[Pipelines](../clients/promtail/pipelines.md) for more information.

An example Grafana dashboard was built by the community and is available as
dashboard [10004](https://grafana.com/dashboards/10004).

## Mixins

The Loki repository has a [mixin](../../production/loki-mixin) that includes a
set of dashboards, recording rules, and alerts. Together, the mixin gives you a
comprehensive package for monitoring Loki in production.

For more information about mixins, take a look at the docs for the
[monitoring-mixins project](https://github.com/monitoring-mixins/docs).


