// imports
local lib = import '../../lib/_imports.libsonnet';
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  qps(component)::
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
          [camelCaseComponent]()
          .label('route').re(config.components[camelCaseComponent].routes)
          .build()
      ],
}
