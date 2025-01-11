// imports
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local treemap = lib.panels.treemap;
local override = lib.panels.helpers.override('stat');
local custom = lib.panels.helpers.custom('stat');
local color = lib.panels.helpers.color('stat');
local transformation = lib.panels.helpers.transformation('stat');
local thresholds = lib.panels.helpers.thresholds('stat');
local step = lib.panels.helpers.step('stat');
{
  new(
    title = 'Route Latency',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    treemap.milliseconds({
      title: title,
      datasource: datasource,
      targets: targets,
    })
    + thresholds.withSteps([
      step.withValue(null) + step.withColor('green'),
      step.withValue(50) + step.withColor('yellow'),
      step.withValue(100) + step.withColor('orange'),
      step.withValue(1000) + step.withColor('red'),
    ])
}
