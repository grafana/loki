// imports
local lib = import '../../lib/_imports.libsonnet';
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  rate(component)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
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
      ||| % [
        selector()
          .component(component)
          .label('route').re(config.components[camelCaseComponent].routes)
          .build()
      ],

  latency_histogram(component)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    if !std.objectHas(config.components, camelCaseComponent) then
      error 'Invalid component: %s, no routes found' % [component]
    else |||
      sum by (le) (
        rate(loki_request_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [
      selector()
        .component(component)
        .label('route').re(config.components[camelCaseComponent].routes)
        .build()
    ],

  latency_percentile(component, percentile, by='route', recording_rule=config.use_recording_rules)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    if !std.objectHas(config.components, camelCaseComponent) then
      error 'Invalid component: %s, no routes found' % [component]
    else
      if recording_rule then |||
        histogram_quantile(
          %g,
          sum by (le, %s) (
            cluster_component_route:loki_request_duration_seconds_bucket:sum_rate{%s}
          )
        ) * 1e3
      ||| % [
        percentile,
        by,
        selector()
            .component(component)
            .label('route').re(config.components[camelCaseComponent].routes)
            .build(),
        ]
      else |||
        histogram_quantile(
          %g,
          sum by (le, %s)(
            rate(loki_request_duration_seconds_bucket{%s}[5m])
          )
        ) * 1e3
      ||| % [
        percentile,
        by,
        selector()
          .component(component)
          .label('route').re(config.components[camelCaseComponent].routes)
          .build(),
      ],

  latency_percentile_99(component, by='route', recording_rule=config.use_recording_rules):: self.latency_percentile(component, 0.99, by, recording_rule),
  latency_percentile_95(component, by='route', recording_rule=config.use_recording_rules):: self.latency_percentile(component, 0.95, by, recording_rule),
  latency_percentile_90(component, by='route', recording_rule=config.use_recording_rules):: self.latency_percentile(component, 0.90, by, recording_rule),
  latency_percentile_50(component, by='route', recording_rule=config.use_recording_rules):: self.latency_percentile(component, 0.50, by, recording_rule),

  latency_average(component, by='route', recording_rule=config.use_recording_rules)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    if !std.objectHas(config.components, camelCaseComponent) then
      error 'Invalid component: %s, no routes found' % [component]
    else
      if recording_rule then |||
        (
          sum by (%s) (
            cluster_job_route:loki_request_duration_seconds_sum:sum_rate{%s}
          )
          /
          sum by (%s) (
            cluster_job_route:loki_request_duration_seconds_count:sum_rate{%s}
          )
        ) * 1e3
      ||| % [
        by,
        selector()
          .component(component)
          .label('route').re(config.components[camelCaseComponent].routes)
          .build(),
        by,
        selector()
          .component(component)
          .label('route').re(config.components[camelCaseComponent].routes)
          .build(),
      ]
      else |||
        (
          sum by (%s) (
            rate(loki_request_duration_seconds_bucket{%s}[$__rate_interval])
          )
          /
          sum by (%s) (
            rate(loki_request_duration_seconds_bucket{%s}[$__rate_interval])
          )
        ) * 1e3
      ||| % [
        by,
        selector()
          .component(component)
          .label('route').re(config.components[camelCaseComponent].routes)
          .build(),
        by,
        selector()
          .component(component)
          .label('route').re(config.components[camelCaseComponent].routes)
          .build(),
      ],
}
