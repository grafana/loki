// imports
local config = import '../../../config.libsonnet';
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new(key)::
    common.panels.timeSeries.throughput(
      title='%s - Disk Reads' % config.components[key].name,
      targets=[
        // disk reads
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.kubernetes.pods.disk_reads(
            component=config.components[key].component,
          ),
          params={
            refId: 'diskReads',
            legendFormat: '{{%s}}' % [config.labels.pod],
          }
        ),
      ],
      datasource=common.variables.metrics_datasource.name,
    ),
}
