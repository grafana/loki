// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.latency(
      title='%s - Per Pod Latency (p99)' % config.components[key].name,
      targets=[
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_p99(
            component=config.components[key].component,
            by='pod',
            recording_rule=false,
          ),
          params={
            refId: 'p99',
            legendFormat: '{{pod}}',
          }
        ),
      ],
      datasource=common.variables.metrics_datasource.name,
    ),
}
