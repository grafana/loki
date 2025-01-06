local g = import 'grizzly/grizzly.libsonnet';
local config = import './config.libsonnet';
local grr = import 'grizzly/grizzly.libsonnet';

// dashboards
local chunks = import './dashboards/chunks.libsonnet';
local overview = import './dashboards/overview.libsonnet';

{
  folders: [
    grr.folder.new(config.folder_slug, config.folder),
  ],
  dashboards: [
    grr.dashboard.new(chunks.uid, chunks) + grr.resource.addMetadata('folder', config.folder_slug),
    grr.dashboard.new(overview.uid, overview) + grr.resource.addMetadata('folder', config.folder_slug),
  ],
  datasources: [],
  prometheus_rule_groups: [],
  sm: [],
}
