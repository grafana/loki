// imports
local config = import '../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local selector = (import '../../selectors.libsonnet').new;
local lib = import '../../../lib/_imports.libsonnet';
{
  _component:: 'bloom-chunks',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

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
}
