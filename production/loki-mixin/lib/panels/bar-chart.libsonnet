// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';
local utils = import '../utils.libsonnet';

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

Base + {
  new(params)::
    local merged = defaultParams + params;
    local optKeys = ['groupWidth', 'barWidth', 'barRadius'];
    // custom
    utils.applyOptions(barChart.options, optKeys, merged)
    + super.new(type = 'barChart', params = merged),
}
