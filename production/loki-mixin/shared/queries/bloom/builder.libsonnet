// imports
local config = import '../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local selector = (import '../../selectors.libsonnet').new;
local lib = import '../../../lib/_imports.libsonnet';

{
  _component:: 'bloom-builder',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

  // Histogram queries for bytes per task
  bytes_per_task_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloombuilder_bytes_per_task_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of bytes per task
  bytes_per_task_p99(by=''):: self.bytes_per_task_percentile(0.99, by),
  // Gets the 95th percentile of bytes per task
  bytes_per_task_p95(by=''):: self.bytes_per_task_percentile(0.95, by),
  // Gets the 90th percentile of bytes per task
  bytes_per_task_p90(by=''):: self.bytes_per_task_percentile(0.90, by),
  // Gets the 50th percentile of bytes per task
  bytes_per_task_p50(by=''):: self.bytes_per_task_percentile(0.50, by),

  // Gets the average bytes per task
  bytes_per_task_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloombuilder_bytes_per_task_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloombuilder_bytes_per_task_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of bytes per task
  bytes_per_task_histogram()::
    |||
      sum by (le) (
        rate(loki_bloombuilder_bytes_per_task_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloombuilder_bytes_per_task_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Histogram queries for chunk series size
  chunk_series_size_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloombuilder_chunk_series_size_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of chunk series size
  chunk_series_size_p99(by=''):: self.chunk_series_size_percentile(0.99, by),
  // Gets the 95th percentile of chunk series size
  chunk_series_size_p95(by=''):: self.chunk_series_size_percentile(0.95, by),
  // Gets the 90th percentile of chunk series size
  chunk_series_size_p90(by=''):: self.chunk_series_size_percentile(0.90, by),
  // Gets the 50th percentile of chunk series size
  chunk_series_size_p50(by=''):: self.chunk_series_size_percentile(0.50, by),

  // Gets the average chunk series size
  chunk_series_size_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloombuilder_chunk_series_size_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloombuilder_chunk_series_size_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of chunk series size
  chunk_series_size_histogram()::
    |||
      sum by (le) (
        rate(loki_bloombuilder_chunk_series_size_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloombuilder_chunk_series_size_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Histogram queries for series per task
  series_per_task_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloombuilder_series_per_task_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of series per task
  series_per_task_p99(by=''):: self.series_per_task_percentile(0.99, by),
  // Gets the 95th percentile of series per task
  series_per_task_p95(by=''):: self.series_per_task_percentile(0.95, by),
  // Gets the 90th percentile of series per task
  series_per_task_p90(by=''):: self.series_per_task_percentile(0.90, by),
  // Gets the 50th percentile of series per task
  series_per_task_p50(by=''):: self.series_per_task_percentile(0.50, by),

  // Gets the average series per task
  series_per_task_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloombuilder_series_per_task_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloombuilder_series_per_task_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of series per task
  series_per_task_histogram()::
    |||
      sum by (le) (
        rate(loki_bloombuilder_series_per_task_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloombuilder_series_per_task_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Counter rates
  // Gets the rate of blocks created
  blocks_created_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloombuilder_blocks_created_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of blocks reused
  blocks_reused_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloombuilder_blocks_reused_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of metas created
  metas_created_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloombuilder_metas_created_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of tasks started
  tasks_started_rate(by='')::
    |||
      sum by (%s) (
        rate(loki_bloombuilder_task_started_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gauge metrics
  // Gets whether a task is being processed
  processing_task(by='')::
    |||
      loki_bloombuilder_processing_task{%s}
    ||| % [self._resourceSelector],

  // Gets whether the builder is running
  running(by='')::
    |||
      loki_bloombuilder_running{%s}
    ||| % [self._resourceSelector],



  // Histogram queries for chunks per block
  chunks_per_block_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_chunks_per_block_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of chunks per block
  chunks_per_block_p99(by=''):: self.chunks_per_block_percentile(0.99, by),
  // Gets the 95th percentile of chunks per block
  chunks_per_block_p95(by=''):: self.chunks_per_block_percentile(0.95, by),
  // Gets the 90th percentile of chunks per block
  chunks_per_block_p90(by=''):: self.chunks_per_block_percentile(0.90, by),
  // Gets the 50th percentile of chunks per block
  chunks_per_block_p50(by=''):: self.chunks_per_block_percentile(0.50, by),

  // Gets the average chunks per block
  chunks_per_block_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_chunks_per_block_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_chunks_per_block_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of chunks per block
  chunks_per_block_histogram()::
    |||
      sum by (le) (
        rate(loki_bloom_chunks_per_block_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_chunks_per_block_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Histogram queries for chunks per series
  chunks_per_series_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_chunks_per_series_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of chunks per series
  chunks_per_series_p99(by=''):: self.chunks_per_series_percentile(0.99, by),
  // Gets the 95th percentile of chunks per series
  chunks_per_series_p95(by=''):: self.chunks_per_series_percentile(0.95, by),
  // Gets the 90th percentile of chunks per series
  chunks_per_series_p90(by=''):: self.chunks_per_series_percentile(0.90, by),
  // Gets the 50th percentile of chunks per series
  chunks_per_series_p50(by=''):: self.chunks_per_series_percentile(0.50, by),

  // Gets the average chunks per series
  chunks_per_series_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_chunks_per_series_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_chunks_per_series_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of chunks per series
  chunks_per_series_histogram()::
    |||
      sum by (le) (
        rate(loki_bloom_chunks_per_series_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_chunks_per_series_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],
}
