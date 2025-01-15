// imports
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';
local shared = import '../../../../shared/_imports.libsonnet';

// local variables
{
  new(
    title = 'Queue Length',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    lib.panels.timeSeries.seconds({
    title: title,
    description: |||
      The query scheduler queue length.
    |||,
    datasource: common.variables.metrics_datasource.name,
    targets: targets,
  })
}
