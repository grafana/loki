// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.cpu(
      title='%s - CPU Usage' % config.components[key].name,
      targets=[
        // cpu usage
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.kubernetes.pods.cpu_usage(
            component=config.components[key].component,
          ),
          params={
            refId: 'usage',
            legendFormat: '{{%s}}' % [config.labels.pod],
          }
        ),
        // cpu requests
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
            expr=shared.queries.kubernetes.pods.cpu_requests(
            component=config.components[key].component,
          ),
          params={
            refId: 'requests',
            legendFormat: 'requests',
          }
        ),
        // cpu limits
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.kubernetes.pods.cpu_limits(
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
