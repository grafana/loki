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

  // Gets the percentile of blocks fetched from store
  blocks_fetched_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_store_blocks_fetched_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component),
    ],

  // Gets the 99th percentile of blocks fetched from store
  blocks_fetched_p99(component, by=''):: self.blocks_fetched_percentile(component, 0.99, by),
  // Gets the 95th percentile of blocks fetched from store
  blocks_fetched_p95(component, by=''):: self.blocks_fetched_percentile(component, 0.95, by),
  // Gets the 90th percentile of blocks fetched from store
  blocks_fetched_p90(component, by=''):: self.blocks_fetched_percentile(component, 0.90, by),
  // Gets the 50th percentile of blocks fetched from store
  blocks_fetched_p50(component, by=''):: self.blocks_fetched_percentile(component, 0.50, by),

  // Gets the average number of blocks fetched from store
  blocks_fetched_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_store_blocks_fetched_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_store_blocks_fetched_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component)], 2),

  // Gets the percentile of block sizes fetched from store in bytes
  blocks_fetched_size_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_store_blocks_fetched_size_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component),
    ],

  // Gets the 99th percentile of block sizes fetched from store in bytes
  blocks_fetched_size_p99(component, by=''):: self.blocks_fetched_size_percentile(component, 0.99, by),
  // Gets the 95th percentile of block sizes fetched from store in bytes
  blocks_fetched_size_p95(component, by=''):: self.blocks_fetched_size_percentile(component, 0.95, by),
  // Gets the 90th percentile of block sizes fetched from store in bytes
  blocks_fetched_size_p90(component, by=''):: self.blocks_fetched_size_percentile(component, 0.90, by),
  // Gets the 50th percentile of block sizes fetched from store in bytes
  blocks_fetched_size_p50(component, by=''):: self.blocks_fetched_size_percentile(component, 0.50, by),

  // Gets the percentile of time spent in download queue in seconds
  download_queue_enqueue_time_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_store_download_queue_enqueue_time_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component),
    ],

  // Gets the 99th percentile of time spent in download queue in seconds
  download_queue_enqueue_time_p99(component, by=''):: self.download_queue_enqueue_time_percentile(component, 0.99, by),
  // Gets the 95th percentile of time spent in download queue in seconds
  download_queue_enqueue_time_p95(component, by=''):: self.download_queue_enqueue_time_percentile(component, 0.95, by),
  // Gets the 90th percentile of time spent in download queue in seconds
  download_queue_enqueue_time_p90(component, by=''):: self.download_queue_enqueue_time_percentile(component, 0.90, by),
  // Gets the 50th percentile of time spent in download queue in seconds
  download_queue_enqueue_time_p50(component, by=''):: self.download_queue_enqueue_time_percentile(component, 0.50, by),

  // Gets the average time spent in download queue in seconds
  download_queue_enqueue_time_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_store_download_queue_enqueue_time_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_store_download_queue_enqueue_time_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component)], 2),

  // Gets the percentile of download queue size
  download_queue_size_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_store_download_queue_size_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component),
    ],

  // Gets the 99th percentile of download queue size
  download_queue_size_p99(component, by=''):: self.download_queue_size_percentile(component, 0.99, by),
  // Gets the 95th percentile of download queue size
  download_queue_size_p95(component, by=''):: self.download_queue_size_percentile(component, 0.95, by),
  // Gets the 90th percentile of download queue size
  download_queue_size_p90(component, by=''):: self.download_queue_size_percentile(component, 0.90, by),
  // Gets the 50th percentile of download queue size
  download_queue_size_p50(component, by=''):: self.download_queue_size_percentile(component, 0.50, by),

  // Gets the average download queue size
  download_queue_size_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_store_download_queue_size_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_store_download_queue_size_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component)], 2),

  // Gets the rate of blocks found by the fetcher
  fetcher_blocks_found_rate(component)::
    |||
      sum(
        rate(loki_bloom_store_fetcher_blocks_found_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component)],

  // Gets the rate of blocks missing from the fetcher
  fetcher_blocks_missing_rate(component)::
    |||
      sum(
        rate(loki_bloom_store_fetcher_blocks_missing_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component)],

  // Gets the percentile of metas fetched from store
  metas_fetched_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_store_metas_fetched_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component),
    ],

  // Gets the 99th percentile of metas fetched from store
  metas_fetched_p99(component, by=''):: self.metas_fetched_percentile(component, 0.99, by),
  // Gets the 95th percentile of metas fetched from store
  metas_fetched_p95(component, by=''):: self.metas_fetched_percentile(component, 0.95, by),
  // Gets the 90th percentile of metas fetched from store
  metas_fetched_p90(component, by=''):: self.metas_fetched_percentile(component, 0.90, by),
  // Gets the 50th percentile of metas fetched from store
  metas_fetched_p50(component, by=''):: self.metas_fetched_percentile(component, 0.50, by),

  // Gets the average number of metas fetched from store
  metas_fetched_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_bloom_store_metas_fetched_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloom_store_metas_fetched_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component)], 2),

  // Gets the percentile of meta sizes fetched from store in bytes
  metas_fetched_size_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloom_store_metas_fetched_size_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component),
    ],

  // Gets the 99th percentile of meta sizes fetched from store in bytes
  metas_fetched_size_p99(component, by=''):: self.metas_fetched_size_percentile(component, 0.99, by),
  // Gets the 95th percentile of meta sizes fetched from store in bytes
  metas_fetched_size_p95(component, by=''):: self.metas_fetched_size_percentile(component, 0.95, by),
  // Gets the 90th percentile of meta sizes fetched from store in bytes
  metas_fetched_size_p90(component, by=''):: self.metas_fetched_size_percentile(component, 0.90, by),
  // Gets the 50th percentile of meta sizes fetched from store in bytes
  metas_fetched_size_p50(component, by=''):: self.metas_fetched_size_percentile(component, 0.50, by),
}
