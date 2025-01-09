/*******************************************************************
* defines the table structure for usage
********************************************************************/

/*******************************************************************
* defines the time series structure for usage
********************************************************************/

// imports
local g = import '../../../../lib/grafana.libsonnet';
local base = import '../../../../lib/panels/table/base.libsonnet';
local thresholds = import '../../thresholds.libsonnet';
local config = import '../../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local panelHelpers = import '../../../../lib/panels/helpers.libsonnet';

// local variables
local table = g.panel.table;
local override = panelHelpers.override('table');
local transformation = panelHelpers.transformation('table');
local standardOptions = panelHelpers.standardOptions('table');
local custom = panelHelpers.custom('table');

local defaultParams = {
  fieldDisplayMode: 'color-text',
  filterable: false,
  enablePagination: true,
  unit: 'short',
  cellHeight: 'md',
  overrides: [
    override.byName.new('Cost')
      + override.byName.withPropertiesFromOptions(
          standardOptions.withUnit('currencyUSD')
          + custom.withWidth(70)
        ),
    override.byName.new('Group')
      + override.byName.withPropertiesFromOptions(
          standardOptions.withLinks([
            {
              targetBlank: true,
              title: 'Drill Down',
              url: '/d/grafanacloud_usage_drilldown/current-usage-4-drill-down?orgId=1&var-groupname=${__data.fields[\"Group\"]}'
            }
          ])
          + custom.withFilterable(true)
        ),
    override.byName.new('Usage')
      + override.byName.withPropertiesFromOptions(
          custom.withCellOptions({
            mode: 'gradient',
            type: 'gauge',
            valueDisplayMode: 'color',
          })
        ),
    override.byName.new('Share')
      + override.byName.withPropertiesFromOptions(
          custom.withWidth(65)
          + standardOptions.withUnit('percentunit')
        ),
    override.byName.new('Log Lines')
      + override.byName.withPropertiesFromOptions(
          custom.withWidth(85)
          + standardOptions.withUnit('short')
        ),
    override.byName.new('Avg. Size')
      + override.byName.withPropertiesFromOptions(
          custom.withWidth(80)
        ),
  ],
};

local serviceDefaults = {
  metrics: {
    transformations: [
      transformation.withId('organize')
        + transformation.withOptions({
            excludeByName: std.foldl(function(acc, item) acc { [item]: true }, config.group_types, {
              Time: true,
              group_label: true
            }),
          indexByName: {},
          renameByName: {
            Value: 'Usage',
            'Value #GroupUsage': 'Usage',
            group: 'Group',
          }
        }),
        transformation.withId('calculateField')
          + transformation.withOptions({
              alias: 'Cost',
              binary: {
                left: 'Usage',
                operator: '*',
                reducer: 'sum',
                right: '${%s}' % variables.cost_per_series.name,
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
                left: 'Usage',
                operator: '/',
                reducer: 'sum',
                right: '${%s}' % variables.metric_series_total.name,
              },
              mode: 'binary',
              reduce: {
                reducer: 'sum'
              }
            }),
    ]
  },
  logs: {
    transformations: [
      transformation.withId('merge'),
      transformation.withId('organize')
        + transformation.withOptions({
            excludeByName: std.foldl(function(acc, item) acc { [item]: true }, config.group_types, {
              Time: true,
              group_label: true
            }),
          indexByName: {},
          renameByName: {
            Value: 'Usage',
            'Value #GroupLines': 'Log Lines',
            'Value #GroupUsage': 'Usage',
            group: 'Group',
          }
        }),
      transformation.withId('calculateField')
        + transformation.withOptions({
            alias: 'Cost',
            binary: {
              left: 'Usage',
              operator: '*',
              reducer: 'sum',
              right: '${%s}' % variables.cost_per_byte.name,
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
              left: 'Usage',
              operator: '/',
              reducer: 'sum',
              right: '${%s}' % variables.log_bytes_total.name,
            },
            mode: 'binary',
            reduce: {
              reducer: 'sum'
            }
          }),
      transformation.withId('calculateField')
        + transformation.withOptions({
            alias: 'Avg. Size',
            binary: {
              left: 'Usage',
              operator: '/',
              reducer: 'sum',
              right: 'Log Lines'
            },
            mode: 'binary',
            reduce: {
              reducer: 'sum'
            }
          }),
      transformation.withId('organize')
        + transformation.withOptions({
            indexByName: {
              Group: 0,
              Cost: 1,
              Usage: 2,
              Share: 3,
              'Log Lines': 4,
              'Avg. Size': 5,
            }
          }),
    ],
  },
};


{
  new(params, service = 'metrics'):: base.new(defaultParams + serviceDefaults[service] + params)
    + thresholds.usage('table', (if std.objectHas(params, 'fixedColor') then params.fixedColor else null))
    + table.options.withSortBy([
      table.options.sortBy.withDisplayName('Cost') + table.options.sortBy.withDesc(true)
    ]),
}
