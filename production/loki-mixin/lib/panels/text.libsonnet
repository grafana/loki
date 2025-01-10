// imports
local g = import '../grafana.libsonnet';
local text = g.panel.text;

// local variables
local panel = g.panel.text;

// local variables
local defaultParams = {
  title: '',
  content: '',
  h: null,
  w: null,
  x: null,
  y: null,
};

{
  new(params)::
    local merged = defaultParams + params;
    panel.new(merged.title)
      + (
        if std.objectHas(merged, 'content') && merged.content != null then
          text.options.withContent(merged.content)
        else
          {}
      )
      + panel.panelOptions.withGridPos(h = merged.h, w = merged.w, x = merged.x, y = merged.y)
      + panel.options.withMode('markdown'),

  header(params)::
    self.new(params + { title: '' })
}
