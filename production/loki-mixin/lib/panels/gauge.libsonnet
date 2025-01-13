// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local gauge = g.panel.gauge;
local defaultParams = {
  showThresholdMarkers: true,
  showThresholdLabels: true,
};

gauge + Base + {
  new(params)::
    super.new(
      type='gauge',
      params=defaultParams + params,
    ),
}
