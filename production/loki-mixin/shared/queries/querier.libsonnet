// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _component:: 'querier',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

  // Gets the percentile of query frontend request duration
  querier_query_frontend_request_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_querier_query_frontend_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of query frontend request duration
  querier_query_frontend_request_p99(by=''):: self.querier_query_frontend_request_percentile(0.99, by),
  // Gets the 95th percentile of query frontend request duration
  querier_query_frontend_request_p95(by=''):: self.querier_query_frontend_request_percentile(0.95, by),
  // Gets the 90th percentile of query frontend request duration
  querier_query_frontend_request_p90(by=''):: self.querier_query_frontend_request_percentile(0.90, by),
  // Gets the 50th percentile of query frontend request duration
  querier_query_frontend_request_p50(by=''):: self.querier_query_frontend_request_percentile(0.50, by),

  // Gets the average query frontend request duration
  querier_query_frontend_request_average(by='')::
    |||
      sum by (%s) (
        rate(loki_querier_query_frontend_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_querier_query_frontend_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of query frontend request duration
  querier_query_frontend_request_histogram()::
    |||
      sum by (le) (
        rate(loki_querier_query_frontend_request_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Counter rates
  // Gets the rate of index cache corruptions
  index_cache_corruptions_rate::
    |||
      sum(
        rate(loki_querier_index_cache_corruptions_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of index cache encode errors
  index_cache_encode_errors_rate::
    |||
      sum(
        rate(loki_querier_index_cache_encode_errors_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of index cache gets
  index_cache_gets_rate::
    |||
      sum(
        rate(loki_querier_index_cache_gets_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of index cache hits
  index_cache_hits_rate::
    |||
      sum(
        rate(loki_querier_index_cache_hits_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of index cache puts
  index_cache_puts_rate::
    |||
      sum(
        rate(loki_querier_index_cache_puts_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of tail bytes processed
  tail_bytes_rate::
    |||
      sum(
        rate(loki_querier_tail_bytes_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gauge metrics
  // Gets the number of query frontend clients
  query_frontend_clients::
    |||
      loki_querier_query_frontend_clients{%s}
    ||| % [self._resourceSelector],

  // Gets the number of active tail queries
  tail_active::
    |||
      loki_querier_tail_active{%s}
    ||| % [self._resourceSelector],

  // Gets the number of active tail streams
  tail_active_streams::
    |||
      loki_querier_tail_active_streams{%s}
    ||| % [self._resourceSelector],

  // Gets the worker concurrency
  worker_concurrency::
    |||
      loki_querier_worker_concurrency{%s}
    ||| % [self._resourceSelector],

  // Gets the number of inflight queries in workers
  worker_inflight_queries::
    |||
      loki_querier_worker_inflight_queries{%s}
    ||| % [self._resourceSelector],

  // Calculated metrics
  // Gets the index cache hit ratio
  index_cache_hit_ratio::
    |||
      sum(rate(loki_querier_index_cache_hits_total{%s}[$__rate_interval]))
      /
      sum(rate(loki_querier_index_cache_gets_total{%s}[$__rate_interval]))
    ||| % std.repeat([self._resourceSelector], 2),
}
