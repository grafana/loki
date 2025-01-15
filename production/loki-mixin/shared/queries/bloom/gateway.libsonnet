// imports
local config = import '../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local selector = (import '../../selectors.libsonnet').new;
local lib = import '../../../lib/_imports.libsonnet';

{
  _component:: 'bloom-gateway',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

  // Histogram queries for block query latency
  block_query_latency_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_gateway_block_query_latency_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of block query latency
  block_query_latency_p99(by=''):: self.block_query_latency_percentile(0.99, by),
  // Gets the 95th percentile of block query latency
  block_query_latency_p95(by=''):: self.block_query_latency_percentile(0.95, by),
  // Gets the 90th percentile of block query latency
  block_query_latency_p90(by=''):: self.block_query_latency_percentile(0.90, by),
  // Gets the 50th percentile of block query latency
  block_query_latency_p50(by=''):: self.block_query_latency_percentile(0.50, by),

  // Gets the average block query latency
  block_query_latency_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_block_query_latency_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_gateway_block_query_latency_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of block query latency
  block_query_latency_histogram()::
    |||
      sum by (le) (
        rate(loki_bloom_gateway_block_query_latency_seconds_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_gateway_block_query_latency_seconds_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Histogram queries for client request duration
  client_request_duration_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_gateway_client_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of client request duration
  client_request_duration_p99(by=''):: self.client_request_duration_percentile(0.99, by),
  // Gets the 95th percentile of client request duration
  client_request_duration_p95(by=''):: self.client_request_duration_percentile(0.95, by),
  // Gets the 90th percentile of client request duration
  client_request_duration_p90(by=''):: self.client_request_duration_percentile(0.90, by),
  // Gets the 50th percentile of client request duration
  client_request_duration_p50(by=''):: self.client_request_duration_percentile(0.50, by),

  // Gets the average client request duration
  client_request_duration_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_client_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_gateway_client_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of client request duration
  client_request_duration_histogram()::
    |||
      sum by (le) (
        rate(loki_bloom_gateway_client_request_duration_seconds_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_gateway_client_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Histogram queries for filtered chunks
  filtered_chunks_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_gateway_filtered_chunks_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of filtered chunks
  filtered_chunks_p99(by=''):: self.filtered_chunks_percentile(0.99, by),
  // Gets the 95th percentile of filtered chunks
  filtered_chunks_p95(by=''):: self.filtered_chunks_percentile(0.95, by),
  // Gets the 90th percentile of filtered chunks
  filtered_chunks_p90(by=''):: self.filtered_chunks_percentile(0.90, by),
  // Gets the 50th percentile of filtered chunks
  filtered_chunks_p50(by=''):: self.filtered_chunks_percentile(0.50, by),

  // Gets the average filtered chunks
  filtered_chunks_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_filtered_chunks_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_gateway_filtered_chunks_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of filtered chunks
  filtered_chunks_histogram()::
    |||
      sum by (le) (
        rate(loki_bloom_gateway_filtered_chunks_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_gateway_filtered_chunks_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Counter rates
  // Gets the rate of chunks filtered
  chunks_filtered_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_querier_chunks_filtered_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of chunks skipped
  chunks_skipped_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_querier_chunks_skipped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of total chunks
  chunks_total_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_querier_chunks_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of series filtered
  series_filtered_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_querier_series_filtered_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of series skipped
  series_skipped_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_querier_series_skipped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of total series
  series_total_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_gateway_querier_series_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gauge metrics
  // Gets the number of inflight tasks
  inflight_tasks(by='')::
    |||
      loki_bloom_gateway_inflight_tasks{%s}
    ||| % [self._resourceSelector],

  // Gets the queue length
  queue_length(by='')::
    |||
      loki_bloom_gateway_queue_length{%s}
    ||| % [self._resourceSelector],

  // Calculated metrics
  // Gets the chunk filter ratio
  chunk_filter_ratio(by='')::
    |||
      sum by (%s) (rate(loki_bloom_gateway_querier_chunks_filtered_total{%s}[$__rate_interval]))
      /
      sum by (%s) (rate(loki_bloom_gateway_querier_chunks_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the series filter ratio
  series_filter_ratio(by='')::
    |||
      sum by (%s) (rate(loki_bloom_gateway_querier_series_filtered_total{%s}[$__rate_interval]))
      /
      sum by (%s) (rate(loki_bloom_gateway_querier_series_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector], 2),
}
