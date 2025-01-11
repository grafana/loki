/*
* helper functions to reduce path length
*/

// imports
local g = import '../grafana.libsonnet';

{
  panel(type)::
    if type == 'treemap' then
      g.panel.stat
    else if std.objectHas(g.panel, type) then
      g.panel[type]
    else
      error 'Unknown panel type',

  standardOptions(type)::
    local panel = self.panel(type);
    if std.objectHas(panel, 'standardOptions') then
      panel.standardOptions
    else
      error 'Panel type does not have standard options',

  thresholds(type)::
    local standardOptions = self.standardOptions(type);
    if std.objectHas(standardOptions, 'thresholds') then
      standardOptions.thresholds
    else
      error 'Panel type does not have thresholds',

  step(type)::
    local standardOptions = self.standardOptions(type);
    if std.objectHas(standardOptions, 'threshold') && std.objectHas(standardOptions.threshold, 'step') then
      standardOptions.threshold.step
    else
      error 'Panel type does not have threshold step',

  override(type)::
    local standardOptions = self.standardOptions(type);
    if std.objectHas(standardOptions, 'override') then
      standardOptions.override
    else
      error 'Panel type does not have standard option override',

  color(type)::
    local standardOptions = self.standardOptions(type);
    if std.objectHas(standardOptions, 'color') then
      standardOptions.color
    else
      error 'Panel type does not have standard option color',

  fieldConfig(type)::
    local panel = self.panel(type);
    if std.objectHas(panel, 'fieldConfig') then
      panel.fieldConfig
    else
      error 'Panel type does not have standard options',

  defaults(type)::
    local fieldConfig = self.fieldConfig(type);
    if std.objectHas(fieldConfig, 'defaults') then
      fieldConfig.defaults
    else
      error 'Panel type does not have field config defaults',

  custom(type)::
    local defaults = self.defaults(type);
    if std.objectHas(defaults, 'custom') then
      defaults.custom
    else
      error 'Panel type does not have field config defaults',

  queryOptions(type)::
    local panel = self.panel(type);
    if std.objectHas(panel, 'queryOptions') then
      panel.queryOptions
    else
      error 'Panel type does not have query options',

  transformation(type)::
    local queryOptions = self.queryOptions(type);
    if std.objectHas(queryOptions, 'transformation') then
      queryOptions.transformation
    else
      error 'Panel type does not have query option transformation',
}
