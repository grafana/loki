// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local table = lib.panels.table;
local override = lib.panels.helpers.override('table');
local custom = lib.panels.helpers.custom('table');
local transformation = lib.panels.helpers.transformation('table');

lib.panels.table.new({
  title: 'Tenant Overrides merged with Defaults',
  description: |||
    Default overrides set by the the [Limits Configuration](https://grafana.com/docs/loki/latest/configure/#limits_config) or are the default value in Loki if not set.

    - [limits_config](https://grafana.com/docs/loki/latest/configure/#limits_config)
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.tenant.merged_overrides,
      {
        format: 'table',
        instant: true,
        refId: 'Merged Overrides',
      }
    ),
  ],
  fieldDisplayMode: 'color-text',
  filterable: false,
  unit: 'short',
  overrides: [
    override.byName.new('Limit Name')
      + override.byName.withPropertiesFromOptions(
          custom.withCellOptions({type: 'json-view'})
          + custom.withFilterable(true)
        ),
    override.byName.new('Value')
      + override.byName.withPropertiesFromOptions(
          custom.withWidth(125)
        ),
  ],
  transformations: [
    transformation.withId('organize')
      + transformation.withOptions({
        "excludeByName": {
          "Time": true,
        },
        "includeByName": {},
        "indexByName": {
          "Time": 2,
          "Value": 3,
          "limit_name": 1,
          "user": 0
        },
        "renameByName": {
          "Value": "Value",
          "limit_name": "Limit Name",
        }
      }),
  ]
})
+ table.options.withSortBy([
    table.options.sortBy.withDisplayName('Limit Name') + table.options.sortBy.withDesc(false)
])
