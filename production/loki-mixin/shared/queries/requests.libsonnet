// imports
local lib = import '../../lib/_imports.libsonnet';
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  // Calculates request rate per status code (2xx, 3xx, 4xx, 5xx) for a given component
  rate(component)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    local routeSelector =
      selector()
        .resource(component)
        .label('route').re(config.components[camelCaseComponent].routes)
        .build();
    if !std.objectHas(config.components, camelCaseComponent) then
      error 'Invalid component: %s, no routes found' % [component]
    else
      |||
        sum by (status) (
          label_replace(
            label_replace(
              rate(loki_request_duration_seconds_count{%s}[$__rate_interval]),
              "status", "${1}xx", "status_code", "([0-9]).."),
            "status", "${1}", "status_code", "([a-zA-Z]+)"
          )
        )
      ||| % [routeSelector],

  // Gets the histogram data for request latencies of a given component
  latency_histogram(component)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    local routeSelector =
      selector()
        .resource(component)
        .label('route').re(config.components[camelCaseComponent].routes)
        .build();
    if !std.objectHas(config.components, camelCaseComponent) then
      error 'Invalid component: %s, no routes found' % [component]
    else
      |||
        sum by (le) (
          rate(loki_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      ||| % [routeSelector],

  // Calculates a specific latency percentile for requests to a component
  latency_percentile(component, percentile, by='route', recording_rule=config.recording_rules.loki)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    if !std.objectHas(config.components, camelCaseComponent) then
      error 'Invalid component: %s, no routes found' % [component]
    else
      if recording_rule then
        local routeSelector =
          selector()
            .component(component)
            .label('route').re(config.components[camelCaseComponent].routes)
            .build();
        |||
          histogram_quantile(
            %g,
            sum by (le, %s) (
              cluster_component_route:loki_request_duration_seconds_bucket:sum_rate{%s}
            )
          ) * 1e3
        ||| % [percentile, by, routeSelector]
      else
        local routeSelector =
          selector()
            .resource(component)
            .label('route').re(config.components[camelCaseComponent].routes)
            .build();
        |||
          histogram_quantile(
            %g,
            sum by (le, %s)(
              rate(loki_request_duration_seconds_bucket{%s}[5m])
            )
          ) * 1e3
        ||| % [percentile, by, routeSelector],

  // Calculates the 99th percentile of request latencies
  latency_percentile_99(component, by='route', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(component, 0.99, by, recording_rule),

  // Calculates the 95th percentile of request latencies
  latency_percentile_95(component, by='route', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(component, 0.95, by, recording_rule),

  // Calculates the 90th percentile of request latencies
  latency_percentile_90(component, by='route', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(component, 0.90, by, recording_rule),

  // Calculates the 50th percentile (median) of request latencies
  latency_percentile_50(component, by='route', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(component, 0.50, by, recording_rule),

  // Calculates the average request latency for a component
  latency_average(component, by='route', recording_rule=config.recording_rules.loki)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    if !std.objectHas(config.components, camelCaseComponent) then
      error 'Invalid component: %s, no routes found' % [component]
    else
      if recording_rule then
        local routeSelector =
          selector()
            .component(component)
            .label('route').re(config.components[camelCaseComponent].routes)
            .build();
        |||
          (
            sum by (%s) (
              cluster_component_route:loki_request_duration_seconds_sum:sum_rate{%s}
            )
            /
            sum by (%s) (
              cluster_component_route:loki_request_duration_seconds_count:sum_rate{%s}
            )
          ) * 1e3
        ||| % [by, routeSelector, by, routeSelector]
      else
        local routeSelector =
          selector()
            .resource(component)
            .label('route').re(config.components[camelCaseComponent].routes)
            .build();
        |||
          (
            sum by (%s) (
              rate(loki_request_duration_seconds_bucket{%s}[$__rate_interval])
            )
            /
            sum by (%s) (
              rate(loki_request_duration_seconds_bucket{%s}[$__rate_interval])
            )
          ) * 1e3
        ||| % [by, routeSelector, by, routeSelector],
}
