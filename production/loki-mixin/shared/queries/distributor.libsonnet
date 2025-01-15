// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    local sel = selector();
    if std.isArray(component) then
      sel.resources(component)
    else
      sel.resource(component),

  // Histogram queries
  // Gets the percentile of Kafka latency
  kafka_latency_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_distributor_kafka_latency_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of Kafka latency
  kafka_latency_p99(component, by=config.labels.resource_selector)::
    self.kafka_latency_percentile(component, 0.99, by),

  // Gets the 95th percentile of Kafka latency
  kafka_latency_p95(component, by=config.labels.resource_selector)::
    self.kafka_latency_percentile(component, 0.95, by),

  // Gets the 90th percentile of Kafka latency
  kafka_latency_p90(component, by=config.labels.resource_selector)::
    self.kafka_latency_percentile(component, 0.90, by),

  // Gets the 50th percentile of Kafka latency
  kafka_latency_p50(component, by=config.labels.resource_selector)::
    self.kafka_latency_percentile(component, 0.50, by),

  // Gets the average Kafka latency
  kafka_latency_average(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_distributor_kafka_latency_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_distributor_kafka_latency_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the percentile of records per write request
  kafka_records_per_request_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_distributor_kafka_records_per_write_request_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the average records per write request
  kafka_records_per_request_average(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_distributor_kafka_records_per_write_request_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_distributor_kafka_records_per_write_request_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Counter rates
  // Gets the rate of bytes received
  bytes_received_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_distributor_bytes_received_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of ingester appends
  ingester_appends_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_distributor_ingester_appends_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of lines received
  lines_received_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_distributor_lines_received_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of Kafka sent bytes
  kafka_sent_bytes_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_distributor_kafka_sent_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of structured metadata bytes received
  structured_metadata_bytes_received_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_distributor_structured_metadata_bytes_received_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gauge metrics
  // Gets the number of ingester clients
  ingester_clients(component, by=config.labels.resource_selector)::
    |||
      loki_distributor_ingester_clients{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the replication factor
  replication_factor(component, by=config.labels.resource_selector)::
    |||
      loki_distributor_replication_factor{%s}
    ||| % [self._resourceSelector(component).build()],

  // Calculated metrics
  // Gets the average bytes per line
  bytes_per_line_average(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (rate(loki_distributor_bytes_received_total{%s}[$__rate_interval]))
      /
      sum by (%s) (rate(loki_distributor_lines_received_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the percentile of KV request duration
  kv_request_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(kv_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of KV request duration
  kv_request_duration_p99(component, by='')::
    self.kv_request_duration_percentile(component, 0.99, by),

  // Gets the 95th percentile of KV request duration
  kv_request_duration_p95(component, by='')::
    self.kv_request_duration_percentile(component, 0.95, by),

  // Gets the 90th percentile of KV request duration
  kv_request_duration_p90(component, by='')::
    self.kv_request_duration_percentile(component, 0.90, by),

  // Gets the 50th percentile of KV request duration
  kv_request_duration_p50(component, by='')::
    self.kv_request_duration_percentile(component, 0.50, by),

  // Gets the average KV request duration
  kv_request_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(kv_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(kv_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of KV request duration
  kv_request_duration_histogram(component)::
    |||
      sum by (le) (
        rate(kv_request_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],
}
