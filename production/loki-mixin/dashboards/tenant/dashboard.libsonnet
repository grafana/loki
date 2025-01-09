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
  uid: 'loki-tenant',
  tags: config.tags + ['tenant', 'overrides'],
  from: 'now-1h',
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
    [
      lib.panels.row.new({
        title: 'Overrides',
      }),
    ]
    +
    grid.makeGrid([
      // Panel: Tenant Overrides
      panels.tenantOverrides,
      // Panel: Merged Overrides
      panels.mergedOverrides,
    ], panelWidth=12, panelHeight=7, startY=1)
    +
    // Row: Throughput
    grid.makeGrid([
      lib.panels.row.new({
        title: 'Throughput',
        y: 15,
      })
      +
      lib.panels.row.withPanels([
        // Panel: Bytes In
        panels.bytesIn,
        // Panel: Structured Metadata Bytes In
        panels.structuredMetadataBytesIn,
        // Panel: Lines In
        panels.linesIn,
        // Panel: Average Line Size
        panels.avgLineSize,
      ]),
    ], panelWidth=6, panelHeight=7, startY=15)
    +
    // Row: Discarded Samples
    grid.makeGrid([
      lib.panels.row.new({
        title: 'Discarded Samples',
        y: 22,
      })
      +
      lib.panels.row.withPanels([
        // Panel: Bytes Discarded
        panels.discardedBytes,
        // Panel: Lines Discarded
        panels.discardedLines,

      ]),
    ], panelWidth=8, panelHeight=7, startY=22)
    +
    // Row: Miscellaneous
    grid.makeGrid([
      lib.panels.row.new({
        title: 'Miscellaneous',
        y: 30,
      })
      +
      lib.panels.row.withPanels([
        // Panel: Active Streams
        panels.activeStreams,
        // Panel: Queue Length
        panels.queueLength,
        // Panel: Cell Queue Duration
        panels.queueDuration,
      ]),
    ], panelWidth=8, panelHeight=7, startY=30)
})
