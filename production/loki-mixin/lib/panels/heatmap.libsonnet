// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local heatmap = g.panel.heatmap;
local defaultParams = {};

heatmap + Base + {
  new(params)::
    super.new(
      type='heatmap',
      params=defaultParams + params,
    ),
}
