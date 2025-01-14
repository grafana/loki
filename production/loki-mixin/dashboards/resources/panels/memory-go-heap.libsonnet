// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.memory(
      title='%s - Memory (go heap inuse)' % config.components[key].name,
      targets=[
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.golang.heap_inuse(
            component=config.components[key].component,
          ),
          params={
            refId: 'heapInuse',
            legendFormat: '{{%s}}' % [config.labels.pod],
          }
        ),
      ],
      datasource=common.variables.metrics_datasource.name,
    ),
}
