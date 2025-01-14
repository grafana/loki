local config = import './config.libsonnet';
local lib = import './lib/_imports.libsonnet';
local grr = import 'grizzly/grizzly.libsonnet';
local dashboards = import './dashboards/_imports.libsonnet';

local folder_slug = lib.utils.slugify(config.folder);

{
  folders: [
    grr.folder.new(folder_slug, config.folder),
  ],
  dashboards: [
    grr.dashboard.new(dashboards[name].uid, dashboards[name]) +
    grr.resource.addMetadata('folder', folder_slug)
    for name in std.objectFields(dashboards)
  ],
  datasources: [],
  prometheus_rule_groups: [],
  sm: [],
}
