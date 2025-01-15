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
    lib.panels.timeSeries.rps({
      title: '%s - QPS' % [config.components[camelCaseComponent].name],
      description: |||
        The number of queries per second for the %s component.

        **\*Requires** query stats to be enabled in the [frontend configuration](https://grafana.com/docs/loki/latest/configure/#frontend)

        ```yaml
        frontend:
          query_stats_enabled: true
        ```

      ||| % [config.components[camelCaseComponent].name],
      datasource: common.variables.metrics_datasource.name,
      targets: [
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryStats.qps(component),
          params={
            refId: 'qps',
            legendFormat: '{{status_code}} - {{type}} - {{ range }}',
          }
        )
      ],
      fillOpacity: 100,
      showPoints: 'never',
      overrides: [
        // 2xx
        override.byRegexp.new('/2\\d+\\d+/')
        + override.byRegexp.withPropertiesFromOptions(
          color.withFixedColor('#56A64B')
        ),
        // 5xx
        override.byRegexp.new('/5\\d+\\d+/')
        + override.byRegexp.withPropertiesFromOptions(
          color.withFixedColor('#E02F44')
        )
      ]
    }),
}
