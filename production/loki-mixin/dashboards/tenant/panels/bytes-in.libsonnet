// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;
local override = lib.panels.helpers.override('timeSeries');
local custom = lib.panels.helpers.custom('timeSeries');
local color = lib.panels.helpers.color('timeSeries');
local transformation = lib.panels.helpers.transformation('timeSeries');

timeSeries.bytesRate({
  title: 'Bytes In',
  description: |||
    The total number of bytes ingested per second by the Tenant.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    shared.queries.tenant.ingestion_bytes_rate,
    shared.queries.tenant.ingestion_bytes_rate_limit,
  ],
  overrides: [
    override.byName.new('Rate Limit')
      + override.byName.withPropertiesFromOptions(
          custom.withLineStyle({
            "dash": [10, 20],
            "fill": "dash",
          })
          + custom.withShowPoints('never')
          + color.withFixedColor('red')
        ),
  ]
})
