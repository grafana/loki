// imports
local config = import '../config.libsonnet';
local lib = import '../../lib/_imports.libsonnet';
local common = import '../common/_imports.libsonnet';
local panels = import './panels/_imports.libsonnet';

// local variables
local defaultPanelWidth = 8;
local defaultPanelHeight = 8;

{
  new(path):: (
    local componentsKeys = [
      key
      for key in std.objectFields(config.components)
      if config.components[key].enabled && std.length(std.find(path, config.components[key].paths)) > 0
    ];
    lib.dashboard.new({
      title: 'Loki / %s / Resources' % [lib.utils.toTitleCase(path)],
      description: '',
      uid: lib.dashboard.generateUid(self.title, config.uid_prefix, config.uid_suffix),
      tags: config.tags + [path] + std.map(function(key) config.components[key].component, componentsKeys),
    from: 'now-%s' % [config.default_lookback],
    to: 'now',
    links: common.links, // TODO: add links to documentation
    variables: [
      common.variables.metrics_datasource,
      common.variables.cluster,
      common.variables.namespace,
    ],
    editable: config.editable,
    panels:
      // create the panels in order
      local panelRows = std.flattenArrays(
        [
          [
            // add the row for the component
            lib.panels.row.new({
              title: config.components[key].name,
            }),
            // add the qps, latency and per pod latency panels
            panels.cpu(key),
            panels.memoryWorkingSet(key),
            panels.memoryGoHeap(key),
          ]
          +
          // if the key is 'ingester' add the disk writes panel
          if key == 'ingester' then
            [
              panels.diskWrites(key),
              panels.diskReads(key),
              panels.memoryStreams(),
            ]
          else
            []
          for key in componentsKeys
        ]
      );

      // create the grid for the panels
      local renderedGrid = lib.grafana.util.grid.makeGrid(panelRows, defaultPanelWidth, defaultPanelHeight);
      renderedGrid,
  })

  ),
}
