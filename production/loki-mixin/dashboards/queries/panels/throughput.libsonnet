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
    common.panels.heatmap.throughput(
      title='%s - Throughput' % [config.components[camelCaseComponent].name],
      targets=[
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryStats.bytes_processed_histogram(component),
        )
      ],
    ),
}
