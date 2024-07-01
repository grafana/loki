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

  grafanaDashboards+:
    {
      'loki-mixin-recording-rules.json': raw +
                                         $.dashboard('Loki / Recording Rules', uid='recording-rules')
                                         .addCluster()
                                         .addNamespace()
                                         .addLog()
                                         .addTag()
                                         + {
                                           templating+: {
                                             list+: [
                                               tenantTemplate,
                                             ],
                                           },
                                         },
    },
}
