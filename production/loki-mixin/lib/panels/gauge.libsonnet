// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';
local thresholds = import '../../common/thresholds.libsonnet';
local utils = import '../utils.libsonnet';

// local variables
local panel = g.panel.gauge;
local step = panel.standardOptions.threshold.step;
local defaultParams = {
  showThresholdMarkers: true,
  showThresholdLabels: true,
};

Base + {
  new(params)::
    local merged = defaultParams + params;
    local optionKeys = ['showThresholdMarkers', 'showThresholdLabels'];
    // table specific settings
    utils.applyOptions(panel.options, optionKeys, merged)
    + super.new(type='gauge', params=merged),
}
