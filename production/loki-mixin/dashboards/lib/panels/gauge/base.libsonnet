// imports
local g = import '../../grafana.libsonnet';
local basePanel = import '../base.libsonnet';
local thresholds = import '../../../common/thresholds.libsonnet';
local utils = import '../../utils.libsonnet';

// local variables
local panel = g.panel.gauge;
local step = panel.standardOptions.threshold.step;
local defaultParams = {
  showThresholdMarkers: true,
  showThresholdLabels: true,
};

{
  new(params)::
    local merged = defaultParams + params;
    local optionKeys = ['showThresholdMarkers', 'showThresholdLabels'];
    // table specific settings
    utils.applyOptions(panel.options, optionKeys, merged)
    + basePanel.new(type = 'gauge', params = merged),

  short(params)::
    self.new(params + { unit: 'short' }),

  percent(params)::
    local opts = {
      unit: 'percentunit',
      mode: 'thresholds',
      calcs: ['lastNotNull'],
      min: 0,
      max: 1,
      noValue: '0%',
    };

    self.new(params + opts)
      + thresholds.utilization('gauge'),

  currency(params)::
    self.new(params + { unit: 'currencyUSD' }),

  gbytes(params)::
    self.new(params + { unit: 'gbytes' }),
}
