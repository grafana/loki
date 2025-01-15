// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local override = lib.panels.helpers.override('time-series');
local custom = lib.panels.helpers.custom('time-series');
local color = lib.panels.helpers.color('time-series');

{
  new(component)::
    local camelCaseComponent = lib.utils.toCamelCase(component);
    lib.panels.timeSeries.bytesRate({
      title: '%s - Chunks Downloaded' % [config.components[camelCaseComponent].name],
      description: |||
        The number of chunks downloaded per second for the %s component.

        **\*Requires** query stats to be enabled in the [frontend configuration](https://grafana.com/docs/loki/latest/configure/#frontend)

        ```yaml
        frontend:
          query_stats_enabled: true
        ```

      ||| % [config.components[camelCaseComponent].name],
      datasource: common.variables.metrics_datasource.name,
      targets: [],
      fillOpacity: 100,
      showPoints: 'never',
      stacking: {
        group: 'chunks',
        mode: "normal"
      }
    }),
}
