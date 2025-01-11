// imports
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local heatmap = lib.panels.heatmap;
local override = lib.panels.helpers.override('heatmap');
local custom = lib.panels.helpers.custom('heatmap');
local color = lib.panels.helpers.color('heatmap');
local transformation = lib.panels.helpers.transformation('heatmap');

{
  new(
    title = 'Latency Distribution',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    lib.panels.heatmap.seconds({
      title: title,
      datasource: datasource,
      targets: targets,
      cellGap: 2,
      maxDataPoints: 100,
      mode: 'scheme',
      scheme: 'RdYlBu',
      legend: {
        show: false,
      },
      cellValues: {
        unit: 'reqps',
      },
      yAxis: {
        unit: 's',
      },
    })
}
