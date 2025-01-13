// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';

// local variables
local text = g.panel.text;
local defaultParams = {
  title: '',
  content: '',
  h: null,
  w: null,
  x: null,
  y: null,
  mode: 'markdown',
};

text + Base + {
  new(params)::
    super.new(
      type='text',
      params=defaultParams + params,
    ),

  header(params)::
    // blank out the title
    self.new(params + { title: '' })
}
