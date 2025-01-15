// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the rate of index build attempts
  build_index_attempts_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_build_index_attempts_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the last successful index build timestamp
  build_index_last_successful_timestamp(component, by='')::
    |||
      max by (%s) (
        loki_tsdb_build_index_last_successful_timestamp_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of head rotation attempts
  head_rotation_attempts_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_head_rotation_attempts_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of head series not found
  head_series_not_found_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_head_series_not_found_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the table download operation duration
  shipper_query_time_table_download_duration(component, by='')::
    |||
      sum by (%s) (
        loki_tsdb_shipper_query_time_table_download_duration_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of shipper query wait time
  shipper_query_wait_time_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_tsdb_shipper_query_wait_time_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of shipper query wait time
  shipper_query_wait_time_p99(component, by='')::
    self.shipper_query_wait_time_percentile(component, 0.99, by),

  // Gets the 95th percentile of shipper query wait time
  shipper_query_wait_time_p95(component, by='')::
    self.shipper_query_wait_time_percentile(component, 0.95, by),

  // Gets the 90th percentile of shipper query wait time
  shipper_query_wait_time_p90(component, by='')::
    self.shipper_query_wait_time_percentile(component, 0.90, by),

  // Gets the 50th percentile of shipper query wait time
  shipper_query_wait_time_p50(component, by='')::
    self.shipper_query_wait_time_percentile(component, 0.50, by),

  // Gets the average shipper query wait time
  shipper_query_wait_time_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_shipper_query_wait_time_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_tsdb_shipper_query_wait_time_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of shipper query wait time
  shipper_query_wait_time_histogram(component)::
    |||
      sum by (le) (
        rate(loki_tsdb_shipper_query_wait_time_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of table sync latency
  shipper_table_sync_latency_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_tsdb_shipper_table_sync_latency_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of table sync latency
  shipper_table_sync_latency_p99(component, by='')::
    self.shipper_table_sync_latency_percentile(component, 0.99, by),

  // Gets the 95th percentile of table sync latency
  shipper_table_sync_latency_p95(component, by='')::
    self.shipper_table_sync_latency_percentile(component, 0.95, by),

  // Gets the 90th percentile of table sync latency
  shipper_table_sync_latency_p90(component, by='')::
    self.shipper_table_sync_latency_percentile(component, 0.90, by),

  // Gets the 50th percentile of table sync latency
  shipper_table_sync_latency_p50(component, by='')::
    self.shipper_table_sync_latency_percentile(component, 0.50, by),

  // Gets the average table sync latency
  shipper_table_sync_latency_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_shipper_table_sync_latency_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_tsdb_shipper_table_sync_latency_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of table sync latency
  shipper_table_sync_latency_histogram(component)::
    |||
      sum by (le) (
        rate(loki_tsdb_shipper_table_sync_latency_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the tables download operation duration
  shipper_tables_download_operation_duration(component, by='')::
    |||
      sum by (%s) (
        loki_tsdb_shipper_tables_download_operation_duration_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of table sync operations
  shipper_tables_sync_operation_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_shipper_tables_sync_operation_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of table upload operations
  shipper_tables_upload_operation_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_shipper_tables_upload_operation_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of WAL truncation attempts
  wal_truncation_attempts_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_tsdb_wal_truncation_attempts_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],
}
