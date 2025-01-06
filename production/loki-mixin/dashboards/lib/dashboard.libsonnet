// imports
local g = import './grafana.libsonnet';
local utils = import './utils.libsonnet';

// local variables
local dashboard = g.dashboard;

// local helper functions
local defaultParams = {
  annotations: null,
  description: null,
  editable: null,
  fiscalYearStartMonth: null,
  links: null,
  panels: null,
  refresh: null,
  style: null,
  tags: null,
  templating: null,
  timezone: null,
  title: null,
  uid: null,
  variables: null,
  weekStart: null,
  from: 'now/M',
  to: 'now',
};

{
  new(params):: (
    local merged = defaultParams + params;
    local dashKeys = ['annotations', 'description', 'editable', 'fiscalYearStartMonth', 'links', 'panels', 'refresh', 'style', 'tags', 'templating', 'timezone', 'title', 'uid', 'variables', 'weekStart'];
    local timeKeys = ['to', 'from'];
    dashboard.new(params.title)
      + (
          if std.objectHas(merged, 'description') && merged.description != null then
            dashboard.withDescription(params.description)
          else
            {}
        )
      + utils.applyOptions(dashboard, dashKeys, merged)
      + utils.applyOptions(dashboard.time, timeKeys, merged)
  ),
}
