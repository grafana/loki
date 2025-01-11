// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.treemap.routes(
      title='%s - Route Latency' % config.components[key].name,
      targets=[
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_percentile_99(config.components[key].component),
          params={
            refId: 'latency',
            legendFormat: '{{le}}',
            instant: true,
            format: 'table',
          }
        )
      ],
    ),
}
