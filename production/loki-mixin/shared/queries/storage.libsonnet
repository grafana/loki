// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the percentile of S3 request duration
  s3_request_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_s3_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of S3 request duration
  s3_request_duration_p99(component, by='')::
    self.s3_request_duration_percentile(component, 0.99, by),

  // Gets the 95th percentile of S3 request duration
  s3_request_duration_p95(component, by='')::
    self.s3_request_duration_percentile(component, 0.95, by),

  // Gets the 90th percentile of S3 request duration
  s3_request_duration_p90(component, by='')::
    self.s3_request_duration_percentile(component, 0.90, by),

  // Gets the 50th percentile of S3 request duration
  s3_request_duration_p50(component, by='')::
    self.s3_request_duration_percentile(component, 0.50, by),

  // Gets the average S3 request duration
  s3_request_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_s3_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_s3_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of S3 request duration
  s3_request_duration_histogram(component)::
    |||
      sum by (le) (
        rate(loki_s3_request_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of bypassed chunk ref lookups
  store_chunk_ref_lookups_bypassed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_store_chunk_ref_lookups_bypassed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of chunks per batch
  store_chunks_per_batch_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_store_chunks_per_batch_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of chunks per batch
  store_chunks_per_batch_p99(component, by='')::
    self.store_chunks_per_batch_percentile(component, 0.99, by),

  // Gets the 95th percentile of chunks per batch
  store_chunks_per_batch_p95(component, by='')::
    self.store_chunks_per_batch_percentile(component, 0.95, by),

  // Gets the 90th percentile of chunks per batch
  store_chunks_per_batch_p90(component, by='')::
    self.store_chunks_per_batch_percentile(component, 0.90, by),

  // Gets the 50th percentile of chunks per batch
  store_chunks_per_batch_p50(component, by='')::
    self.store_chunks_per_batch_percentile(component, 0.50, by),

  // Gets the average chunks per batch
  store_chunks_per_batch_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_store_chunks_per_batch_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_store_chunks_per_batch_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of chunks per batch
  store_chunks_per_batch_histogram(component)::
    |||
      sum by (le) (
        rate(loki_store_chunks_per_batch_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the congestion control limit
  store_congestion_control_limit(component, by='')::
    |||
      sum by (%s) (
        loki_store_congestion_control_limit{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of congestion control retries
  store_congestion_control_retries_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_store_congestion_control_retries_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of Azure blob egress bytes
  azure_blob_egress_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_azure_blob_egress_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],
}
