/*******************************************************************
* defines the treemap structure for usage
********************************************************************/

// imports
local g = import '../../grafana.libsonnet';
local base = import '../../../../lib/panels/treemap/base.libsonnet';
local config = import '../../../../config.libsonnet';
local thresholds = import '../../../common/thresholds.libsonnet';
local variables = import '../../variables.libsonnet';
local panelHelpers = import '../../../../lib/panels/helpers.libsonnet';

// local variables
local override = panelHelpers.override('stat');
local transformation = panelHelpers.transformation('stat');
local standardOptions = panelHelpers.standardOptions('stat');

local defaultParams = {
  unit: 'currencyUSD',
  labelBy: config.group_label,
  colorByField: 'Value',
  sizeField: 'Value',
  labelFields: [
    'Cost',
    'Share',
  ],
  overrides: [
    override.byName.new('Cost')
      + override.byName.withPropertiesFromOptions(
          standardOptions.withUnit('currencyUSD')
        ),
    override.byName.new('Share')
      + override.byName.withPropertiesFromOptions(
          standardOptions.withUnit('percentunit')
        ),
  ],
};

local serviceDefaults = {
  metrics: {},
  logs: {},
};

{
  new(params, service = 'metrics')::
    local transformations = {
      transformations: [
        transformation.withId('calculateField')
          + transformation.withOptions({
            alias: 'Cost',
            binary: {
              left: 'Value',
              operator: '*',
              reducer: 'sum',
              right: '${%s}' % (if service == 'logs' then variables.cost_per_byte.name else variables.cost_per_series.name),
            },
            mode: 'binary',
            reduce: {
              reducer: 'sum'
            }
          }),
        transformation.withId('calculateField')
          + transformation.withOptions({
            alias: 'Share',
            binary: {
              left: 'Value',
              operator: '/',
              reducer: 'sum',
              right: '${%s}' % (if service == 'logs' then variables.log_bytes_total.name else variables.metric_series_total.name),
            },
            mode: 'binary',
            reduce: {
              reducer: 'sum'
            }
          }),
      ],
    };
    base.new(defaultParams + serviceDefaults[service] + transformations + params)
      + thresholds.usage('stat', (if std.objectHas(params, 'fixedColor') then params.fixedColor else null)),
}
