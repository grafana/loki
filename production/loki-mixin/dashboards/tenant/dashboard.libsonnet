// imports
local config = import '../config.libsonnet';
local lib = import '../../lib/_imports.libsonnet';
local shared = import '../../shared/_imports.libsonnet';
local common = import '../common/_imports.libsonnet';
local panels = import './panels/_imports.libsonnet';

// local variables
local g = lib.grafana;
local grid = g.util.grid;

lib.dashboard.new({
  title: 'Loki / Tenant',
  description: |||
    Per-Tenant overrides set by the [Runtime Configuration](https://grafana.com/docs/loki/latest/configure/#runtime-configuration-file), the metrics are exposed by the [Loki Overrides Exporter](https://grafana.com/docs/loki/latest/operations/overrides-exporter/) component.

    This component is not enabled by default in the helm chart, to enable add the following to your `values.yaml` file:

    ```yaml
    overridesExporter:
      enabled: true
    ```
  |||,
  uid: lib.dashboard.generateUid(self.title, config.uid_prefix, config.uid_suffix),
  tags: config.tags + ['tenant', 'overrides'],
  from: 'now-%s' % [config.default_lookback],
  to: 'now',
  links: common.links, // TODO: add links to documentation
  variables: [
    common.variables.metrics_datasource,
    common.variables.logs_datasource,
    common.variables.cluster,
    common.variables.namespace,
    common.variables.tenant,
  ],
  editable: config.editable,
  panels:
    // Row: Overrides
    grid.makeGrid([
      lib.panels.row.new({
        title: 'Overrides',
      }),
      panels.tenantOverrides,
      panels.mergedOverrides,
    ], panelWidth=12, panelHeight=7, startY=1)
    +
    // Row: Throughput
    grid.makeGrid([
      lib.panels.row.new({
        title: 'Throughput',
        panels: [
          panels.bytesIn,
          panels.structuredMetadataBytesIn,
          panels.linesIn,
          panels.avgLineSize,
        ]
      }),
    ], panelWidth=6, panelHeight=7, startY=15)
    +
    grid.makeGrid([
      lib.panels.row.new({
        title: 'Discarded Samples',
        panels: [
          panels.discardedBytes,
          panels.discardedLines,
        ]
      })
    ], panelWidth=8, panelHeight=7, startY=22)
    +
    grid.makeGrid([
      lib.panels.row.new({
        title: 'Miscellaneous',
        panels: [
          panels.activeStreams,
          panels.queueLength,
          panels.queueDuration,
        ]
      }),
    ], panelWidth=8, panelHeight=7, startY=30)
})
