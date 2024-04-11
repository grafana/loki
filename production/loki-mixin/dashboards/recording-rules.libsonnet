local raw = (import './dashboard-recording-rules.json');
local utils = import 'mixin-utils/utils.libsonnet';
local template = import 'grafonnet/template.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local tenantTemplate =
    template.new(
      'tenant',
      '$datasource',
      'query_result(sum by (id) (grafanacloud_logs_instance_info) and sum(label_replace(loki_tenant:active_streams{' + $._config.per_cluster_label + '="$cluster",namespace="$namespace"},"id","$1","tenant","(.*)")) by(id))',
      regex='/"([^"]+)"/',
      sort=1,
      includeAll=true,
      allValues='.+',
    ),

  grafanaDashboards+: {
    local dashboards = self,
    'loki-mixin-recording-rules.json': {
                                         showMultiCluster:: true,
                                       } + raw +
                                       $.dashboard('Loki / Recording Rules', uid='recording-rules')
                                       .addCluster()
                                       .addNamespace()
                                       .addLog()
                                       .addTag()
                                       + {
                                           panels: [
                                             p {
                                               targets: [
                                                 e {
                                                   expr: if dashboards['loki-mixin-recording-rules.json'].showMultiCluster
                                                   then std.strReplace(super.expr, 'cluster="$cluster", ', $._config.per_cluster_label + '=~"$cluster", ')
                                                   else std.strReplace(super.expr, $._config.per_cluster_label + '="$cluster", ', ''),
                                                 }
                                                 for e in p.targets
                                               ],
                                             }
                                             for p in super.panels
                                           ],
                                         templating+: {
                                           list+: [
                                             tenantTemplate,
                                           ],
                                         },
                                       },
    },
}
