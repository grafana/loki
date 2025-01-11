// imports
local g = import '../grafana.libsonnet';
local utils = import '../utils.libsonnet';

// local variables
local row = g.panel.row;

// local helper functions
local defaultParams = {
  title: null,
  collapsed: false,
  h: null,
  w: null,
  x: null,
  y: null,
  repeat: null,
  panels: null,
};

{
  new(params):: (
    local merged = defaultParams + params;
    local rowKeys = std.filter(
      function(key) key != 'datasource' && key != 'type',
      utils.keyNamesFromMethods(row)
    );
    local gridKeys = ['h', 'w', 'x', 'y'];

    if std.objectHas(merged, 'title') then
      row.new(merged.title)
        + utils.applyOptions(row, rowKeys, merged)
        + utils.applyOptions(row.gridPos, gridKeys, merged)
    else
      error 'Row title is required'
  ),
}
