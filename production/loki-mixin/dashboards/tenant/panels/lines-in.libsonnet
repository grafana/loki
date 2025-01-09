// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;

timeSeries.cps({
  title: 'Lines In',
  description: |||
    The total number of lines ingested by the Tenant.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    shared.queries.tenant.ingestion_lines_rate,
  ],
})
