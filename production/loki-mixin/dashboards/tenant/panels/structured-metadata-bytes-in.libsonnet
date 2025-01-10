// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;

timeSeries.bytesRate({
  title: 'Structured Metadata Bytes In',
  description: |||
    The total number of structured metadata bytes ingested per second by the Tenant.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.tenant.structured_metadata_bytes_rate,
      {
        refId: 'Structured Metadata Bytes In',
        legendFormat: 'Bytes',
      }
    ),
  ],
})
