// imports
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;
local override = lib.panels.helpers.override('timeSeries');
local custom = lib.panels.helpers.custom('timeSeries');
local color = lib.panels.helpers.color('timeSeries');
local transformation = lib.panels.helpers.transformation('timeSeries');

{
  new(
    title = 'Latency',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    lib.panels.timeSeries.milliseconds({
      title: title,
      datasource: datasource,
      targets: targets,
    })
    + custom.withFillOpacity(10)
    + custom.withLineWidth(1)
    + custom.withShowPoints('never')
}
