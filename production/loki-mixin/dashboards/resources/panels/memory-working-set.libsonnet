// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.memory(
      title='%s - Memory Working Set' % config.components[key].name,
      targets=[
        // memory usage
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.kubernetes.pods.memory_usage(
            component=config.components[key].component,
          ),
          params={
            refId: 'memoryUsage',
            legendFormat: '{{%s}}' % [config.labels.pod],
          }
        ),
        // requests
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.kubernetes.pods.memory_requests(
            component=config.components[key].component,
          ),
          params={
            refId: 'requests',
            legendFormat: 'requests',
          }
        ),
        // limits
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.kubernetes.pods.memory_limits(
            component=config.components[key].component,
          ),
          params={
            refId: 'limits',
            legendFormat: 'limits',
          }
        ),
      ],
      datasource=common.variables.metrics_datasource.name,
    ),
}
