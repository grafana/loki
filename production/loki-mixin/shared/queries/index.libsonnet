// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    local sel = selector();
    if std.isArray(component) then
      sel.resources(component)
    else
      sel.resource(component),

  // Histogram queries
  // Gets the percentile of request duration
  request_duration_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_index_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of request duration
  request_duration_p99(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.99, by),

  // Gets the 95th percentile of request duration
  request_duration_p95(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.95, by),

  // Gets the 90th percentile of request duration
  request_duration_p90(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.90, by),

  // Gets the 50th percentile of request duration
  request_duration_p50(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.50, by),

  // Gets the average request duration
  request_duration_average(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_index_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_index_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),
}
