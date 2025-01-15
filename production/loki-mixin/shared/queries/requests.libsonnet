// imports
local lib = import '../../lib/_imports.libsonnet';
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  _resourceSelector(component)::
    local sel = selector();
    if std.isArray(component) then
      sel.resources(component)
    else
      sel.resource(component),

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
  latency_p99(component, by='route', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(component, 0.99, by, recording_rule),

  // Calculates the 95th percentile of request latencies
  latency_p95(component, by='route', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(component, 0.95, by, recording_rule),

  // Calculates the 90th percentile of request latencies
  latency_p90(component, by='route', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(component, 0.90, by, recording_rule),

  // Calculates the 50th percentile (median) of request latencies
  latency_p50(component, by='route', recording_rule=config.recording_rules.loki)::
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

  // Gets the percentile of request message bytes
  message_bytes_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_request_message_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of request message bytes
  message_bytes_p99(component, by=''):: self.message_bytes_percentile(component, 0.99, by),
  // Gets the 95th percentile of request message bytes
  message_bytes_p95(component, by=''):: self.message_bytes_percentile(component, 0.95, by),
  // Gets the 90th percentile of request message bytes
  message_bytes_p90(component, by=''):: self.message_bytes_percentile(component, 0.90, by),
  // Gets the 50th percentile of request message bytes
  message_bytes_p50(component, by=''):: self.message_bytes_percentile(component, 0.50, by),

  // Gets the average request message bytes
  message_bytes_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_request_message_bytes_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_request_message_bytes_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of request message bytes
  message_bytes_histogram(component)::
    |||
      sum by (le) (
        rate(loki_request_message_bytes_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of response message bytes
  response_message_bytes_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_response_message_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of response message bytes
  response_message_bytes_p99(component, by=''):: self.response_message_bytes_percentile(component, 0.99, by),
  // Gets the 95th percentile of response message bytes
  response_message_bytes_p95(component, by=''):: self.response_message_bytes_percentile(component, 0.95, by),
  // Gets the 90th percentile of response message bytes
  response_message_bytes_p90(component, by=''):: self.response_message_bytes_percentile(component, 0.90, by),
  // Gets the 50th percentile of response message bytes
  response_message_bytes_p50(component, by=''):: self.response_message_bytes_percentile(component, 0.50, by),

  // Gets the average response message bytes
  response_message_bytes_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_response_message_bytes_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_response_message_bytes_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of response message bytes
  response_message_bytes_histogram(component)::
    |||
      sum by (le) (
        rate(loki_response_message_bytes_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],
}
