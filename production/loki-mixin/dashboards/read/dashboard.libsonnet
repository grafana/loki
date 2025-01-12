// imports
local config = import '../config.libsonnet';
local lib = import '../../lib/_imports.libsonnet';
local common = import '../common/_imports.libsonnet';
local panels = import './panels/_imports.libsonnet';

// local variables
local grid = lib.grafana.util.grid;

local componentsKeys = [
  key
  for key in std.objectFields(config.components)
  if config.components[key].enabled && std.length(std.find('read', config.components[key].paths)) > 0
];

local rowHeight = 1;
local panelHeight = 7;

lib.dashboard.new({
  title: 'Loki / Read',
  description: '',
  uid: '%s-loki-read' % [config.uid_prefix],
  tags: config.tags + ['read'] + std.map(function(key) config.components[key].component, componentsKeys),
  from: 'now-%s' % [config.default_lookback],
  to: 'now',
  links: common.links, // TODO: add links to documentation
  variables: [
    common.variables.metrics_datasource,
    common.variables.logs_datasource,
    common.variables.cluster,
    common.variables.namespace,
  ],
  editable: config.editable,
  panels:
    std.foldl(
      function(acc, key)
        acc
        + grid.makeGrid([
        // add the row for the component
          lib.panels.row.new({
            title: config.components[key].name,
          }),
        // add the qps, latency and per pod latency panels
          panels.qps(key),
          panels.latency(key),
          panels.perPodLatency(key),
        ], panelWidth=8, panelHeight=panelHeight, startY=std.length(acc) * (rowHeight + panelHeight) + 1)

        // add the latency distribution heatmap and route treemap
        + grid.makeGrid([
          panels.latencyDistribution(key),
        ], panelWidth=12, panelHeight=panelHeight + 1, startY=std.length(acc) * (rowHeight + panelHeight * 2) + 1),
      componentsKeys,
      []
    )
})
