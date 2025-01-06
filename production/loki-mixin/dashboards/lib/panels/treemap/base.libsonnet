// imports
local g = import '../../grafana.libsonnet';

// local variables
local panel = g.panel;

// imports
local basePanel = import '../base.libsonnet';

// local variables
local defaultOptions = {
  colorByField: 'Value',
  sizeField: 'Value',
  textField: '',
  tiling: 'treemapSquarify',
  labelFields: [],
  groupByField: '',
};
local defaultParams = {} + defaultOptions;


// treemap isn't a native plugin, so we start w/ stat panel and then add custom overrides
{
  new(params)::
    local merged = defaultParams + params;
    std.mergePatch(
      basePanel.new(type = 'stat', params = merged)
      + { type: 'marcusolsson-treemap-panel' },
      {
        options: {
          colorByField: merged.colorByField,
          sizeField: merged.sizeField,
          textField: merged.textField,
          tiling: merged.tiling,
          labelFields: merged.labelFields,
          groupByField: merged.groupByField,
        }
      }
    ),

  short(params)::
    self.new(params + { unit: 'short' }),

  percent(params)::
    self.new(params + { unit: 'percent' }),

  currency(params)::
    self.new(params + { unit: 'currencyUSD' }),

  gbytes(params)::
    self.new(params + { unit: 'gbytes' }),

}
