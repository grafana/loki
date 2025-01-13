// imports
local Base = import './_base.libsonnet';

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
    super.new(
      type='barChart',
      params=defaultParams + params,
    ),
}
