local config = import './config.libsonnet';
local grr = import 'grizzly/grizzly.libsonnet';
local dashboards = import './dashboards/_imports.libsonnet';

{
  folders: [
    grr.folder.new(config.folder_slug, config.folder),
  ],
  dashboards: [
    grr.dashboard.new(dashboards[name].uid, dashboards[name]) +
    grr.resource.addMetadata('folder', config.folder_slug)
    for name in std.objectFields(dashboards)
  ],
  datasources: [],
  prometheus_rule_groups: [],
  sm: [],
}
