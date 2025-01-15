// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector::
    selector()
      .resource('ingester')
        .build(),

  // Histogram queries
  // Gets the percentile of blocks per chunk
  blocks_per_chunk_percentile(percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ingester_blocks_per_chunk_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the percentile of chunk age
  chunk_age_percentile(percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ingester_chunk_age_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the percentile of chunk compression ratio
  chunk_compression_ratio_percentile(percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ingester_chunk_compression_ratio_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the percentile of chunk encode time
  chunk_encode_time_percentile(percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ingester_chunk_encode_time_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the percentile of chunk size
  chunk_size_percentile(percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ingester_chunk_size_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the percentile of chunk utilization
  chunk_utilization_percentile(percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ingester_chunk_utilization_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Counter rates
  // Gets the rate of chunks created
  chunks_created_rate(by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ingester_chunks_created_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of chunks flushed
  chunks_flushed_rate(by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ingester_chunks_flushed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of chunks stored
  chunks_stored_rate(by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ingester_chunks_stored_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of streams created
  streams_created_rate(by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ingester_streams_created_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of streams removed
  streams_removed_rate(by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ingester_streams_removed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of WAL records logged
  wal_records_logged_rate(by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ingester_wal_records_logged_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gauge metrics
  // Gets the number of chunks in memory
  memory_chunks(by=config.labels.resource_selector)::
    |||
      loki_ingester_memory_chunks{%s}
    ||| % [self._resourceSelector],

  // Gets the number of streams in memory
  memory_streams(by=config.labels.resource_selector)::
    |||
      loki_ingester_memory_streams{%s}
    ||| % [self._resourceSelector],

  // Gets the size of streams labels in memory
  memory_streams_labels_bytes(by=config.labels.resource_selector)::
    |||
      loki_ingester_memory_streams_labels_bytes{%s}
    ||| % [self._resourceSelector],

  // Gets the number of streams not owned
  not_owned_streams(by=config.labels.resource_selector)::
    |||
      loki_ingester_not_owned_streams{%s}
    ||| % [self._resourceSelector],

  // Gets the length of flush queue
  flush_queue_length(by=config.labels.resource_selector)::
    |||
      loki_ingester_flush_queue_length{%s}
    ||| % [self._resourceSelector],

  // Gets whether limiter is enabled
  limiter_enabled(by=config.labels.resource_selector)::
    |||
      loki_ingester_limiter_enabled{%s}
    ||| % [self._resourceSelector],

  // Gets whether WAL replay is active
  wal_replay_active(by=config.labels.resource_selector)::
    |||
      loki_ingester_wal_replay_active{%s}
    ||| % [self._resourceSelector],

  // Gets the WAL bytes in use
  wal_bytes_in_use(by=config.labels.resource_selector)::
    |||
      loki_ingester_wal_bytes_in_use{%s}
    ||| % [self._resourceSelector],

  // Calculated metrics
  // Gets the chunk flush success ratio
  chunk_flush_ratio(by=config.labels.resource_selector)::
    |||
      sum by (%s) (rate(loki_ingester_chunks_flushed_total{%s}[$__rate_interval]))
      /
      (
        sum by (%s) (rate(loki_ingester_chunks_flushed_total{%s}[$__rate_interval]))
        +
        sum by (%s) (rate(loki_ingester_chunks_flush_failures_total{%s}[$__rate_interval]))
      )
    ||| % std.repeat([by, self._resourceSelector], 3),

  // Gets the average chunk size
  chunk_average_size(by=config.labels.resource_selector)::
    |||
      sum by (%s) (loki_ingester_chunk_stored_bytes_total{%s})
      /
      sum by (%s) (loki_ingester_chunks_stored_total{%s})
    ||| % std.repeat([by, self._resourceSelector], 2),
}
