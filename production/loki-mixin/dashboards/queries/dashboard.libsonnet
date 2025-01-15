// imports
local config = import '../config.libsonnet';
local lib = import '../../lib/_imports.libsonnet';
local shared = import '../../shared/_imports.libsonnet';
local common = import '../common/_imports.libsonnet';
local panels = import './panels/_imports.libsonnet';

// local variables
local defaultPanelWidth = 12;
local defaultPanelHeight = 7;

lib.dashboard.new({
  title: 'Loki / Queries',
  uid: lib.dashboard.generateUid(self.title, config.uid_prefix, config.uid_suffix),
  tags: config.tags + ['queries', 'query-frontend', 'query-scheduler', 'querier'],
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
    // create the panels in order
    local panelRows =
      // Row: Query Frontend
      lib.grafana.util.grid.wrapPanels([
        lib.panels.row.new({
          title: 'Query Frontend',
        }),
        common.panels.timeSeries.queueDuration(),
        common.panels.timeSeries.queueLength(),
        panels.frontend_retries,
      ], panelHeight=7, panelWidth=8)
      + lib.grafana.util.grid.wrapPanels([
        panels.frontend_sharded_queries,
        panels.frontend_partitions,
        panels.shard_factor,
        panels.sharding_parsed_queries,
      ], panelHeight=7, panelWidth=12)

      // Row: Queries
      + [
          lib.panels.row.new({
            title: 'Queries',
            collapsed: true,
            panels:
              lib.grafana.util.grid.wrapPanels([
                panels.qps('query-frontend'),
                panels.latency('query-frontend'),
                panels.chunks_downloaded('query-frontend'),
                panels.qps('querier'),
                panels.latency('querier'),
                panels.chunks_downloaded('querier'),
              ], panelHeight=7, panelWidth=8)
              + lib.grafana.util.grid.wrapPanels([
                panels.throughput_percentiles('query-frontend'),
                panels.throughput('query-frontend'),
                panels.throughput_percentiles('querier'),
                panels.throughput('querier'),
              ], panelHeight=7, panelWidth=6)
          }),
      ];

    // create the grid for the panels
    local renderedGrid = lib.grafana.util.grid.wrapPanels(panelRows, defaultPanelWidth, defaultPanelHeight);
    renderedGrid,
})
