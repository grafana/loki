local config = import '../config.libsonnet';
local g = import './lib/grafana.libsonnet';
local links = import './common/links.libsonnet';
local variables = import './common/variables.libsonnet';
local panels = import './lib/panels.libsonnet';
local queries = import './common/queries/main.libsonnet';
local dashboard = import './lib/dashboard.libsonnet';
local override = (import './lib/override.libsonnet').new;
local color = import './lib/color.libsonnet';
local common = import './common/main.libsonnet';

// local variables
local grid = g.util.grid;

dashboard.new({
  title: 'Loki / Gateway',
  description: '',
  uid: 'loki-gateway',
  tags: config.tags + ['gateway'],
  from: 'now-1h',
  to: 'now',
  links: links,
  variables: [
    variables.metrics_datasource,
    variables.logs_datasource,
    variables.cluster,
    variables.namespace,
  ],
  editable: config.editable,
  panels:
    [
      panels.row.new({
        title: 'Overview',
      })
    ]
})
