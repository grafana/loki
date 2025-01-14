// imports
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';

{
  new(
    title = 'Memory',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    lib.panels.timeSeries.bytes({
      title: title,
      datasource: datasource,
      targets: targets,
      overrides: common.overrides.requestLimits('time-series'),
      fillOpacity: 10,
      showPoints: 'never',
      displayMode: 'table',
      calcs: ['max', 'mean', 'p95'],
      sortBy: '95th %',
      sortDesc: true,
    })
}
