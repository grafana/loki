// imports
local g = import '../../grafana.libsonnet';
local utils = import '../../utils.libsonnet';

// local variables
local timeSeries = g.panel.timeSeries;
local fieldOverride = g.panel.timeSeries.fieldOverride;
local custom = timeSeries.fieldConfig.defaults.custom;
local options = timeSeries.options;
local stdOpts = timeSeries.standardOptions;
local pnlOpts = timeSeries.panelOptions;
local qryOpts = timeSeries.queryOptions;

// imports
local basePanel = import '../base.libsonnet';

// local variables
local defaultParams = {
  lineWidth: 2,
  mode: 'palette-classic',
  pointSize: null,
  showPoints: null,
  spanNulls: null,
  lineStyle: null,
};

{
  new(params)::
    local merged = defaultParams + params;
    local customKeys = ['pointSize', 'showPoints', 'spanNulls', 'lineStyle', 'lineWidth'];
    // custom
    utils.applyOptions(timeSeries.fieldConfig.defaults.custom, customKeys, merged) +
    basePanel.new(type = 'timeSeries', params = merged),

  short(params)::
    self.new(params + { unit: 'short' }),

  percent(params)::
    self.new(params + { unit: 'percent' }),

  currency(params)::
    self.new(params + { unit: 'currencyUSD' }),

  bytes(params)::
    self.new(params + { unit: 'bytes' }),

  bytesRate(params)::
    self.new(params + { unit: 'binBps' }),

  gbytes(params)::
    self.new(params + { unit: 'gbytes' }),

  // count per second
  cps(params)::
    self.new(params + { unit: 'cps' }),

  seconds(params)::
    self.new(params + { unit: 's' }),
}
