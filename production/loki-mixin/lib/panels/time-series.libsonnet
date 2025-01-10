// imports
local g = import '../grafana.libsonnet';
local utils = import '../utils.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local timeSeries = g.panel.timeSeries;
local fieldOverride = g.panel.timeSeries.fieldOverride;
local custom = timeSeries.fieldConfig.defaults.custom;
local options = timeSeries.options;
local stdOpts = timeSeries.standardOptions;
local pnlOpts = timeSeries.panelOptions;
local qryOpts = timeSeries.queryOptions;

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
    utils.applyOptions(timeSeries.fieldConfig.defaults.custom, customKeys, merged) +
    super.new(type = 'timeSeries', params = merged),
}
