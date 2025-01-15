// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the rate of expired streams
  expired_streams_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_rate_store_expired_streams_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the maximum stream rate in bytes
  max_stream_rate_bytes(component, by='')::
    |||
      sum by (%s) (
        loki_rate_store_max_stream_rate_bytes{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the maximum stream shards
  max_stream_shards(component, by='')::
    |||
      sum by (%s) (
        loki_rate_store_max_stream_shards{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the maximum unique stream rate in bytes
  max_unique_stream_rate_bytes(component, by='')::
    |||
      sum by (%s) (
        loki_rate_store_max_unique_stream_rate_bytes{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of refresh duration
  refresh_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_rate_store_refresh_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of refresh duration
  refresh_duration_p99(component, by=''):: self.refresh_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of refresh duration
  refresh_duration_p95(component, by=''):: self.refresh_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of refresh duration
  refresh_duration_p90(component, by=''):: self.refresh_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of refresh duration
  refresh_duration_p50(component, by=''):: self.refresh_duration_percentile(component, 0.50, by),

  // Gets the average refresh duration
  refresh_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_rate_store_refresh_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_rate_store_refresh_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of refresh duration
  refresh_duration_histogram(component)::
    |||
      sum by (le) (
        rate(loki_rate_store_refresh_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of refresh failures
  refresh_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_rate_store_refresh_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of stream rate in bytes
  stream_rate_bytes_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_rate_store_stream_rate_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of stream rate in bytes
  stream_rate_bytes_p99(component, by=''):: self.stream_rate_bytes_percentile(component, 0.99, by),
  // Gets the 95th percentile of stream rate in bytes
  stream_rate_bytes_p95(component, by=''):: self.stream_rate_bytes_percentile(component, 0.95, by),
  // Gets the 90th percentile of stream rate in bytes
  stream_rate_bytes_p90(component, by=''):: self.stream_rate_bytes_percentile(component, 0.90, by),
  // Gets the 50th percentile of stream rate in bytes
  stream_rate_bytes_p50(component, by=''):: self.stream_rate_bytes_percentile(component, 0.50, by),

  // Gets the average stream rate in bytes
  stream_rate_bytes_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_rate_store_stream_rate_bytes_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_rate_store_stream_rate_bytes_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of stream rate in bytes
  stream_rate_bytes_histogram(component)::
    |||
      sum by (le) (
        rate(loki_rate_store_stream_rate_bytes_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of stream shards
  stream_shards_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_rate_store_stream_shards_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of stream shards
  stream_shards_p99(component, by=''):: self.stream_shards_percentile(component, 0.99, by),
  // Gets the 95th percentile of stream shards
  stream_shards_p95(component, by=''):: self.stream_shards_percentile(component, 0.95, by),
  // Gets the 90th percentile of stream shards
  stream_shards_p90(component, by=''):: self.stream_shards_percentile(component, 0.90, by),
  // Gets the 50th percentile of stream shards
  stream_shards_p50(component, by=''):: self.stream_shards_percentile(component, 0.50, by),

  // Gets the average stream shards
  stream_shards_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_rate_store_stream_shards_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_rate_store_stream_shards_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of stream shards
  stream_shards_histogram(component)::
    |||
      sum by (le) (
        rate(loki_rate_store_stream_shards_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the total number of streams
  streams(component, by='')::
    |||
      sum by (%s) (
        loki_rate_store_streams{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],
}
