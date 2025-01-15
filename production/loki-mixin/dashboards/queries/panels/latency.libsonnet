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
      title: '%s - Latency' % [config.components[camelCaseComponent].name],
      description: |||
        The latency in seconds of the %s component.

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
          expr=shared.queries.queryStats.latency_p99(component),
          params={
            refId: 'p99',
            legendFormat: '{{ type }} - {{ range }} (p99)',
          }
        ),

        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryStats.latency_p95(component),
          params={
            refId: 'p95',
            legendFormat: '{{ type }} - {{ range }} (p95)',
          }
        ),

        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryStats.latency_p90(component),
          params={
            refId: 'p90',
            legendFormat: '{{ type }} - {{ range }} (p90)',
          }
        ),

        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryStats.latency_p50(component),
          params={
            refId: 'p50',
            legendFormat: '{{ type }} - {{ range }} (p50)',
          }
        ),

        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryStats.latency_average(component),
          params={
            refId: 'avg',
            legendFormat: '{{ type }} - {{ range }} (avg)',
          }
        )
      ],
      fillOpacity: 10,
      showPoints: 'never',
      displayMode: 'table',
      sortBy: 'Name',
    }),
}
