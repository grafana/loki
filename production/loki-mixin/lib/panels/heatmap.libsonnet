// imports
local g = import '../../grafana.libsonnet';
local utils = import '../../utils.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local heatmap = g.panel.heatmap;
local custom = heatmap.fieldConfig.defaults.custom;
local options = heatmap.options;
local stdOpts = heatmap.standardOptions;
local pnlOpts = heatmap.panelOptions;
local qryOpts = heatmap.queryOptions;

// local variables
local defaultParams = {
  lineWidth: 2,
  mode: 'palette-classic',
  pointSize: null,
  showPoints: null,
  spanNulls: null,
  lineStyle: null,
};

Base + {
  new(params)::
    local merged = defaultParams + params;
    local customKeys = ['pointSize', 'showPoints', 'spanNulls', 'lineStyle', 'lineWidth'];
    // custom
    utils.applyOptions(heatmap.fieldConfig.defaults.custom, customKeys, merged) +
    super.new(type = 'heatmap', params = merged),
}
