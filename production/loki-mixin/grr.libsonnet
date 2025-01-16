local grr = import 'grizzly/grizzly.libsonnet';
local mixin = import 'mixin.libsonnet';

{
  folders: [
    grr.folder.new(std.asciiLower(mixin.grafanaDashboardFolder), mixin.grafanaDashboardFolder),
  ],
  dashboards: [
    grr.dashboard.new(mixin.grafanaDashboards[d].uid, mixin.grafanaDashboards[d]) +
    grr.resource.addMetadata('folder', mixin.grafanaDashboardFolder)
    for d in std.objectFields(mixin.grafanaDashboards)
  ],
  datasources: [],
  prometheus_rule_groups: [
    grr.prometheus.rule_group.new(std.asciiLower(mixin.grafanaDashboardFolder), group.name, { rules: group.rules })
    for rules in [mixin.prometheusAlerts.groups, mixin.prometheusRules.groups]
    for group in rules
  ],
  sm: [],
}
