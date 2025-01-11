// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';
{
  queue_duration_percentile(percentile)::
    |||
      histogram_quantile(
        %g,
        sum by (le) (
          rate(loki_query_scheduler_queue_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      selector().queryScheduler().build(),
    ],

  queue_duration_percentile_99: self.queue_duration_percentile(0.99),
  queue_duration_percentile_95: self.queue_duration_percentile(0.95),
  queue_duration_percentile_90: self.queue_duration_percentile(0.90),
  queue_duration_percentile_50: self.queue_duration_percentile(0.50),

  queue_duration_average:
    |||
      sum(
        rate(loki_query_scheduler_queue_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum(
        rate(loki_query_scheduler_queue_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([selector().queryScheduler().build()], 2),

  queue_length:
    |||
      loki_query_scheduler_queue_length{%s}
    ||| % [
      selector().tenant().queryScheduler().build(),
    ],
}
