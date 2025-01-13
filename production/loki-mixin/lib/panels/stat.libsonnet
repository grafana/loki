// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local stat = g.panel.stat;
local defaultParams = {};

stat + Base + {
  new(params)::
    super.new(
      type='stat',
      params=defaultParams + params,
    ),

  single(params)::
    local singleParams = {
      graphMode: 'none',
      colorMode: 'value',
      mode: 'fixed',
    };
    super.new(
      type='stat',
      params=defaultParams + singleParams + params,
    ),

  text(params)::
    self.single(params + {
      colorMode: 'background',
      noValue:  (
        if std.objectHas(params, 'title') then
          params.title
        else if std.objectHas(params, 'noValue') then
          params.noValue
        else
          null
      ),
    }),
}
