// imports
local config = import '../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local selector = (import '../../selectors.libsonnet').new;
local lib = import '../../../lib/_imports.libsonnet';
{
  _resourceSelector(component)::
    selector()
      .resource(component)
      .build(),

  // Gets the rate of series iterated in blocks
  series_iterated_rate(component)::
    |||
      sum(
        rate(loki_bloom_block_series_iterated_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component)],

  // Histogram queries for block size
  size_percentile(percentile, by='', component='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_block_size_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component),
    ],

  // Gets the 99th percentile of block size
  size_p99(by='', component=''):: self.size_percentile(0.99, by, component),
  // Gets the 95th percentile of block size
  size_p95(by='', component=''):: self.size_percentile(0.95, by, component),
  // Gets the 90th percentile of block size
  size_p90(by='', component=''):: self.size_percentile(0.90, by, component),
  // Gets the 50th percentile of block size
  size_p50(by='', component=''):: self.size_percentile(0.50, by, component),

  // Gets the average block size
  size_average(by='', component='')::
    |||
      sum by (%s) (
        rate(loki_bloom_block_size_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_block_size_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component)], 2),

  // Gets the histogram of block size
  size_histogram(component)::
    |||
      sum by (le) (
        rate(loki_bloom_block_size_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_block_size_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component)],

  // Cache metrics
  // Gets the rate of blocks added to cache
  cache_added_rate(component)::
    |||
      sum(
        rate(loki_bloom_blocks_cache_added_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component)],

  // Gets the current number of cache entries
  cache_entries(component)::
    |||
      loki_bloom_blocks_cache_entries{%s}
    ||| % [self._resourceSelector(component)],

  // Gets the rate of blocks fetched from cache
  cache_fetched_rate(component)::
    |||
      sum(
        rate(loki_bloom_blocks_cache_fetched_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component)],

  // Gets the current cache usage in bytes
  cache_usage_bytes(component)::
    |||
      loki_bloom_blocks_cache_usage_bytes{%s}
    ||| % [self._resourceSelector(component)],
}
