local config = import '../config.libsonnet';

local g = import '../lib/grafana.libsonnet';
local link = g.dashboard.link.link;

// docs: https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/manage-dashboard-links/#dashboard-links
[
  link.new('Loki Dashboards', '')
    + link.options.withAsDropdown(true)
    + link.options.withIncludeVars(true)
    + link.options.withKeepTime(true)
    + link.options.withTargetBlank(false)
  + {
    tags: config.link_tags,
    type: 'dashboards',
  }
]
