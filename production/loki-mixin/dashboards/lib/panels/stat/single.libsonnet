/*******************************************************************
* defines the single stat panel
********************************************************************/

// imports
local base = import './base.libsonnet';

// local variables
local defaultParams = {
  graphMode: 'none',
  colorMode: 'value',
  mode: 'fixed',
};

{
  _single(params)::
    base.new(defaultParams + params),

  short(params)::
    self._single(params + { unit: 'short' }),

  percent(params)::
    self._single(params + { unit: 'percent' }),

  currency(params)::
    self._single(params + { unit: 'currencyUSD' }),

  gbytes(params)::
    self._single(params + { unit: 'gbytes' }),

  text(params)::
    self._single(params + {
      colorMode: 'background',
      noValue:  (
        if std.objectHas(params, 'title') then
          params.title
        else if std.objectHas(params, 'noValue') then
          params.noValue
        else
          null
      ),
      title: '',
    }),

}
