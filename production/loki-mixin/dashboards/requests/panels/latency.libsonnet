// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.latency(
      title='%s - Latency' % config.components[key].name,
      datasource=common.variables.metrics_datasource.name,
      params={
        lineWidth: 2,
        fillOpacity: 30,
        tooltip: {
          mode: 'multi',
          sort: 'none',
        }
      },
      targets=[
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_percentile_99(
            component=config.components[key].component,
            by='',
          ),
          params={
            refId: 'p99',
            legendFormat: 'p99',
          }
        ),
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_percentile_95(
            component=config.components[key].component,
            by='',
          ),
          params={
            refId: 'p95',
            legendFormat: 'p95',
          }
        ),
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_percentile_90(
            component=config.components[key].component,
            by='',
          ),
          params={
            refId: 'p90',
            legendFormat: 'p90',
          }
        ),
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_percentile_50(
            component=config.components[key].component,
            by='',
          ),
          params={
            refId: 'p50',
            legendFormat: 'p50',
          }
        ),
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.requests.latency_average(
            component=config.components[key].component,
            by='',
          ),
          params={
            refId: 'avg',
            legendFormat: 'avg',
          }
        ),
      ],
    ),
}
