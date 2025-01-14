// imports

local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.qps(
      title='%s - QPS' % config.components[key].name,
      targets=[
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.rate(config.components[key].component),
          params={
            refId: 'qps',
            legendFormat: '{{status}}',
          }
        ),
      ],
      datasource=common.variables.metrics_datasource.name,
    ),
}
