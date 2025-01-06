/*******************************************************************
* defines the time series structure for usage
********************************************************************/

// imports
local base = import '../../../lib/panels/time-series/base.libsonnet';

// local variables
local defaultParams = {
  tooltip: {
    mode: 'multi',
    sort: 'desc'
  },
  calcs: ['max'],
  displayMode: 'table',
  interval: '1h',
  unit: 'currencyUSD',
  showPoints: 'never',
  sortBy: 'Total',
  sortDesc: true,
};

local serviceDefaults = {
  metrics: {},
  logs: {
    calcs: ['min', 'max', 'mean', 'sum'],
  }
};

{
  new(params, service = 'metrics'): base.new(defaultParams + serviceDefaults[service] + params),
}
