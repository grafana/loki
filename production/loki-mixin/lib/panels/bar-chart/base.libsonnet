// imports
local g = import '../../grafana.libsonnet';
local basePanel = import '../base.libsonnet';
local utils = import '../../utils.libsonnet';

// local variables
local barChart = g.panel.barChart;

// local variables
local defaultParams = {
  groupWidth: 0.85,
  barWidth: 0.97,
  barRadius: 0.05,
  tooltip: {
    mode: 'multi',
    sort: 'none',
  },
};

{
  new(params)::
    local merged = defaultParams + params;
    local optKeys = ['groupWidth', 'barWidth', 'barRadius'];
    // custom
    utils.applyOptions(barChart.options, optKeys, merged)
    + basePanel.new(type = 'barChart', params = merged),

  short(params)::
    self.new(params + { unit: 'short' }),

  percent(params)::
    self.new(params + { unit: 'percent' }),

  currency(params)::
    self.new(params + { unit: 'currencyUSD' }),

  gbytes(params)::
    self.new(params + { unit: 'gbytes' }),
}
