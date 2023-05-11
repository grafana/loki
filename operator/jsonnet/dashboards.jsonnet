local cfg = (import 'config.libsonnet');
local loki = (import 'github.com/grafana/loki/production/loki-mixin/mixin.libsonnet') + cfg.loki;

{
  'grafana-dashboard-lokistack-chunks.json': loki.grafanaDashboards['loki-chunks.json'],
  'grafana-dashboard-lokistack-reads.json': loki.grafanaDashboards['loki-reads.json'],
  'grafana-dashboard-lokistack-writes.json': loki.grafanaDashboards['loki-writes.json'],
  'grafana-dashboard-lokistack-retention.json': loki.grafanaDashboards['loki-retention.json'],
  'grafana-dashboard-lokistack-rules.json': loki.prometheusRules,
}
