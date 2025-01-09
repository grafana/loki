// imports
local basePanel = import '../base.libsonnet';

// local variables
local defaultParams = {};

{
  new(params)::
    local merged = defaultParams + params;
    basePanel.new(type = 'stat', params = merged),

  short(params)::
    self.new(params + { unit: 'short' }),

  percent(params)::
    self.new(params + { unit: 'percent' }),

  currency(params)::
    self.new(params + { unit: 'currencyUSD' }),

  gbytes(params)::
    self.new(params + { unit: 'gbytes' }),
}
+ {
  single: import './single.libsonnet'
}
