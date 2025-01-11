// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.latency(
      title='%s - Latency' % config.components[key].name,
      targets=[
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_percentile_99(config.components[key].component),
          params={
            refId: 'p99',
            legendFormat: '{{route}} 99th percentile',
          }
        ),
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_percentile_95(config.components[key].component),
          params={
            refId: 'p50',
            legendFormat: '{{route}} 50th percentile',
          }
        ),
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_average(config.components[key].component),
          params={
            refId: 'avg',
            legendFormat: '{{route}} average',
          }
        ),
      ],
      datasource=common.variables.metrics_datasource.name,
    ),
}
