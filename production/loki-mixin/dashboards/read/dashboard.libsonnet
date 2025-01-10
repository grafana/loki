// imports
local config = import '../config.libsonnet';
local lib = import '../../lib/_imports.libsonnet';
local shared = import '../../shared/_imports.libsonnet';
local common = import '../common/_imports.libsonnet';
local panels = import './panels/_imports.libsonnet';

// local variables
local g = lib.grafana;
local grid = g.util.grid;

local components = [
  'gateway',
  'query-frontend',
  'querier',
  'ingester',
  'index-gateway',
  'bloom-gateway'
];

local rowHeight = 1;
local panelHeight = 7;
local panelWidth = 8;

lib.dashboard.new({
  title: 'Loki / Read',
  description: '',
  uid: 'loki-read',
  tags: config.tags + ['read'],
  from: 'now-1h',
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
      function(acc, component)
        acc + grid.makeGrid([
          lib.panels.row.new({
            title: component,
          }),
          common.panels.timeSeries.qps(
            targets=[
              lib.query.prometheus.new(
                common.variables.metrics_datasource.name,
                shared.queries.read.qps(component),
                {
                  refId: 'qps',
                  legendFormat: '{{status}}',
                }
              )
            ],
            datasource=common.variables.metrics_datasource.name,
          ),
        ], panelWidth=panelWidth, panelHeight=panelHeight, startY=std.length(acc) * (rowHeight + panelHeight) + 1),
      components,
      []
    )
})
