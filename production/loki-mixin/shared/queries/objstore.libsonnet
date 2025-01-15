// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the last successful upload time
  bucket_last_successful_upload_time(component, by='')::
    |||
      sum by (%s) (
        loki_objstore_bucket_last_successful_upload_time{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of operation duration
  bucket_operation_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_objstore_bucket_operation_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of operation duration
  bucket_operation_duration_p99(component, by=''):: self.bucket_operation_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of operation duration
  bucket_operation_duration_p95(component, by=''):: self.bucket_operation_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of operation duration
  bucket_operation_duration_p90(component, by=''):: self.bucket_operation_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of operation duration
  bucket_operation_duration_p50(component, by=''):: self.bucket_operation_duration_percentile(component, 0.50, by),

  // Gets the average operation duration
  bucket_operation_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_objstore_bucket_operation_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_objstore_bucket_operation_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of operation duration
  bucket_operation_duration_histogram(component)::
    |||
      sum by (le) (
        rate(loki_objstore_bucket_operation_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of operation failures
  bucket_operation_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_objstore_bucket_operation_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of bytes fetched
  bucket_operation_fetched_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_objstore_bucket_operation_fetched_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of transferred bytes
  bucket_operation_transferred_bytes_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_objstore_bucket_operation_transferred_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of transferred bytes
  bucket_operation_transferred_bytes_p99(component, by=''):: self.bucket_operation_transferred_bytes_percentile(component, 0.99, by),
  // Gets the 95th percentile of transferred bytes
  bucket_operation_transferred_bytes_p95(component, by=''):: self.bucket_operation_transferred_bytes_percentile(component, 0.95, by),
  // Gets the 90th percentile of transferred bytes
  bucket_operation_transferred_bytes_p90(component, by=''):: self.bucket_operation_transferred_bytes_percentile(component, 0.90, by),
  // Gets the 50th percentile of transferred bytes
  bucket_operation_transferred_bytes_p50(component, by=''):: self.bucket_operation_transferred_bytes_percentile(component, 0.50, by),

  // Gets the average transferred bytes
  bucket_operation_transferred_bytes_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_objstore_bucket_operation_transferred_bytes_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_objstore_bucket_operation_transferred_bytes_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of transferred bytes
  bucket_operation_transferred_bytes_histogram(component)::
    |||
      sum by (le) (
        rate(loki_objstore_bucket_operation_transferred_bytes_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of operations
  bucket_operations_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_objstore_bucket_operations_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the operation success ratio
  bucket_operation_success_ratio(component, by='')::
    |||
      (
        sum by (%s) (rate(loki_objstore_bucket_operations_total{%s}[$__rate_interval]))
        -
        sum by (%s) (rate(loki_objstore_bucket_operation_failures_total{%s}[$__rate_interval]))
      )
      /
      sum by (%s) (rate(loki_objstore_bucket_operations_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 3),
}
