// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;

timeSeries.bytesRate({
  title: 'Bytes Discarded',
  description: |||
    The total number of bytes discarded per second by the Tenant.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    shared.queries.tenant.discarded_bytes_rate,
  ],
})
