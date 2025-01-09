local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  local it = self,
  queue_duration_percentile(percentile = 0.99, type = 'range')::
    common.queryType(type)
      + common.promql.withExpr(|||
        histogram_quantile(
          %g,
          sum(
            rate(loki_query_scheduler_queue_duration_seconds_bucket{%s}[$__rate_interval])
          ) by (le)
        )
      ||| % [
        percentile,
        selector()
          .queryScheduler()
          .build()
      ])
      + common.promql.withRefId('p%g' % (percentile * 100))
      + common.promql.withLegendFormat('p%g' % (percentile * 100)),

  queue_duration_percentile_99:
    it.queue_duration_percentile(0.99),

  queue_duration_percentile_95:
    it.queue_duration_percentile(0.95),

  queue_duration_percentile_90:
    it.queue_duration_percentile(0.90),

  queue_duration_percentile_50:
    it.queue_duration_percentile(0.50),

  queue_duration_average:
    common.queryType('range')
      + common.promql.withExpr(|||
        sum(
          rate(loki_query_scheduler_queue_duration_seconds_sum{%s}[$__rate_interval])
        )
        /
        sum(
          rate(loki_query_scheduler_queue_duration_seconds_count{%s}[$__rate_interval])
        )
      ||| % [
        selector()
          .queryScheduler()
          .build(),
        selector()
          .queryScheduler()
          .build()
      ])
      + common.promql.withRefId('Queue Duration Average')
      + common.promql.withLegendFormat('avg'),

  queue_length:
    common.queryType('range')
      + common.promql.withExpr(|||
        loki_query_scheduler_queue_length{%s, component=~"query-scheduler|((enterprise|loki)-)?read"}
      ||| % [
        selector().queryScheduler().build()
      ])
      + common.promql.withRefId('Queue Length')
      + common.promql.withLegendFormat('Queue Length'),
}
