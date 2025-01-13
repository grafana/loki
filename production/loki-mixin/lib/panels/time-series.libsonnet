// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local timeSeries = g.panel.timeSeries;
local defaultParams = {
  lineWidth: 1,
  mode: 'palette-classic',
  pointSize: null,
  showPoints: null,
  spanNulls: null,
  lineStyle: null,
};

timeSeries + Base + {
  new(params)::
    super.new(
      type='timeSeries',
      params=defaultParams + params,
    ),
}
