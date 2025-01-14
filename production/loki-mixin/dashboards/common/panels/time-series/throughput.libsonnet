// imports
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';

{
  new(
    title = 'Disk Writes',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    lib.panels.timeSeries.bytesRate({
      title: title,
      datasource: datasource,
      targets: targets,
      fillOpacity: 100,
      showPoints: 'never',
      displayMode: 'table',
      calcs: ['max', 'mean', 'p95'],
      sortBy: '95th %',
      sortDesc: true,
  })
}
