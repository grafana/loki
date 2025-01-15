// imports
local config = import '../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local selector = (import '../../selectors.libsonnet').new;
local lib = import '../../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the percentile of estimated count in the bloom filter
  estimated_count_percentile(component, percentile, by='')::
    |||
      histogram_quantile(%g, sum by (le, %s) (rate(loki_bloom_estimated_count_bucket{%s}[$__rate_interval])))
    ||| % [percentile, by, self._resourceSelector(component).build()],

  // Gets the 99th percentile of estimated count
  estimated_count_p99(component, by=''):: self.estimated_count_percentile(component, 0.99, by),
  // Gets the 95th percentile of estimated count
  estimated_count_p95(component, by=''):: self.estimated_count_percentile(component, 0.95, by),
  // Gets the 90th percentile of estimated count
  estimated_count_p90(component, by=''):: self.estimated_count_percentile(component, 0.90, by),
  // Gets the 50th percentile of estimated count
  estimated_count_p50(component, by=''):: self.estimated_count_percentile(component, 0.50, by),

  // Gets the average estimated count
  estimated_count_average(component, by='')::
    |||
      sum by (%s) (rate(loki_bloom_estimated_count_sum{%s}[$__rate_interval])) / sum by (%s) (rate(loki_bloom_estimated_count_count{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the percentile of hamming weight ratio
  hamming_weight_ratio_percentile(component, percentile, by='')::
    |||
      histogram_quantile(%g, sum by (le, %s) (rate(loki_bloom_hamming_weight_ratio_bucket{%s}[$__rate_interval])))
    ||| % [percentile, by, self._resourceSelector(component).build()],

  // Gets the 99th percentile of hamming weight ratio
  hamming_weight_ratio_p99(component, by=''):: self.hamming_weight_ratio_percentile(component, 0.99, by),
  // Gets the 95th percentile of hamming weight ratio
  hamming_weight_ratio_p95(component, by=''):: self.hamming_weight_ratio_percentile(component, 0.95, by),
  // Gets the 90th percentile of hamming weight ratio
  hamming_weight_ratio_p90(component, by=''):: self.hamming_weight_ratio_percentile(component, 0.90, by),
  // Gets the 50th percentile of hamming weight ratio
  hamming_weight_ratio_p50(component, by=''):: self.hamming_weight_ratio_percentile(component, 0.50, by),

  // Gets the average hamming weight ratio
  hamming_weight_ratio_average(component, by='')::
    |||
      sum by (%s) (rate(loki_bloom_hamming_weight_ratio_sum{%s}[$__rate_interval])) / sum by (%s) (rate(loki_bloom_hamming_weight_ratio_count{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of chunks recorded
  recorder_chunks_rate(component)::
    |||
      sum(rate(loki_bloom_recorder_chunks_total{%s}[$__rate_interval]))
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of series per block
  series_per_block_percentile(component, percentile, by='')::
    |||
      histogram_quantile(%g, sum by (le, %s) (rate(loki_bloom_series_per_block_bucket{%s}[$__rate_interval])))
    ||| % [percentile, by, self._resourceSelector(component).build()],

  // Gets the 99th percentile of series per block
  series_per_block_p99(component, by=''):: self.series_per_block_percentile(component, 0.99, by),
  // Gets the 95th percentile of series per block
  series_per_block_p95(component, by=''):: self.series_per_block_percentile(component, 0.95, by),
  // Gets the 90th percentile of series per block
  series_per_block_p90(component, by=''):: self.series_per_block_percentile(component, 0.90, by),
  // Gets the 50th percentile of series per block
  series_per_block_p50(component, by=''):: self.series_per_block_percentile(component, 0.50, by),

  // Gets the average series per block
  series_per_block_average(component, by='')::
    |||
      sum by (%s) (rate(loki_bloom_series_per_block_sum{%s}[$__rate_interval])) / sum by (%s) (rate(loki_bloom_series_per_block_count{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of tokens processed
  tokens_rate(component)::
    |||
      sum(rate(loki_bloom_tokens_total{%s}[$__rate_interval]))
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of blooms created
  blooms_created_rate(component)::
    |||
      sum(rate(loki_blooms_created_total{%s}[$__rate_interval]))
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of bloom filter size
  size_percentile(component, percentile, by='')::
    |||
      histogram_quantile(%g, sum by (le, %s) (rate(loki_bloom_size_bucket{%s}[$__rate_interval])))
    ||| % [percentile, by, self._resourceSelector(component).build()],

  // Gets the 99th percentile of bloom filter size
  size_p99(component, by=''):: self.size_percentile(component, 0.99, by),
  // Gets the 95th percentile of bloom filter size
  size_p95(component, by=''):: self.size_percentile(component, 0.95, by),
  // Gets the 90th percentile of bloom filter size
  size_p90(component, by=''):: self.size_percentile(component, 0.90, by),
  // Gets the 50th percentile of bloom filter size
  size_p50(component, by=''):: self.size_percentile(component, 0.50, by),

  // Gets the average bloom filter size
  size_average(component, by='')::
    |||
      sum by (%s) (rate(loki_bloom_size_sum{%s}[$__rate_interval])) / sum by (%s) (rate(loki_bloom_size_count{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of bytes added to the bloom filter source
  source_bytes_added_rate(component)::
    |||
      sum(rate(loki_bloom_source_bytes_added_total{%s}[$__rate_interval]))
    ||| % [self._resourceSelector(component).build()],
}
