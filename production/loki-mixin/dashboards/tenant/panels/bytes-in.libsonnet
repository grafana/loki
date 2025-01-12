// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';
local config = import '../../../config.libsonnet';
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
    lib.query.prometheus.new(
      datasource=common.variables.metrics_datasource.name,
      expr=shared.queries.tenant.ingestion_bytes_rate(recording_rule=config.use_recording_rules),
      params={
        refId: 'Bytes Rate',
        legendFormat: 'Bytes',
      }
    ),
    lib.query.prometheus.new(
      datasource=common.variables.metrics_datasource.name,
      expr=shared.queries.tenant.ingestion_bytes_rate_limit,
      params={
        refId: 'Rate Limit',
        legendFormat: 'Rate Limit',
      }
    ),
  ],
  overrides: [
    override.byName.new('Rate Limit')
      + override.byName.withPropertiesFromOptions(
          custom.withLineStyle({
            "dash": [10, 20],
            "fill": "dash",
          })
          + color.withMode('fixed')
          + color.withFixedColor('red')
          + custom.withShowPoints('never')
        ),
  ]
})
