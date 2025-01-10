// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local panel = g.panel;

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
Base + {
  new(params)::
    local merged = defaultParams + params;
    std.mergePatch(
      super.new(type = 'stat', params = merged)
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
}
