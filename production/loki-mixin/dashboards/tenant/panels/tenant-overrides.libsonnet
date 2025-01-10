// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local table = lib.panels.table;
local override = lib.panels.helpers.override('table');
local custom = lib.panels.helpers.custom('table');
local transformation = lib.panels.helpers.transformation('table');

table.new({
  title: 'Tenant Overrides',
  description: |||
    Per-Tenant [overrides](https://grafana.com/docs/loki/latest/configure/#limits_config) set by the [Runtime Configuration](https://grafana.com/docs/loki/latest/configure/#runtime-configuration-file), the metrics are exposed by the [Loki Overrides Exporter](https://grafana.com/docs/loki/latest/operations/overrides-exporter/) component.

    This component is not enabled by default in the helm chart, to enable add the following to your `values.yaml` file:

    ```yaml
    overridesExporter:
      enabled: true
    ```
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.tenant.overrides,
      {
        format: 'table',
        instant: true,
        refId: 'Tenant Overrides',
      }
    ),
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.tenant.default_overrides,
      {
        format: 'table',
        instant: true,
        refId: 'Default Overrides',
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
    override.byName.new('Override')
      + override.byName.withPropertiesFromOptions(
          custom.withWidth(125)
        ),
    override.byName.new('Default')
      + override.byName.withPropertiesFromOptions(
          custom.withWidth(125)
        ),
  ],
  transformations: [
    transformation.withId('joinByField')
      + transformation.withOptions({
        "byField": "limit_name",
        "mode": "inner"
      }),
    transformation.withId('organize')
      + transformation.withOptions({
        "excludeByName": {
          "Time 1": true,
          "Time 2": true,
          "user": true,
        },
        "includeByName": {},
        "indexByName": {
          "Time 1": 2,
          "Time 2": 4,
          "Value #Default Overrides": 5,
          "Value #Tenant Overrides": 3,
          "limit_name": 1,
          "user": 0,
        },
        "renameByName": {
          "Value #Default Overrides": "Default",
          "Value #Tenant Overrides": "Override",
          "limit_name": "Limit Name",
        }
      }),
  ]
})
+ table.options.withSortBy([
    table.options.sortBy.withDisplayName('Limit Name') + table.options.sortBy.withDesc(false)
])
