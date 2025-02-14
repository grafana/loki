local raw = (import './dashboard-recording-rules.json');
local template = import 'grafonnet/template.libsonnet';
local selector = (import '../selectors.libsonnet').new;

(import 'dashboard-utils.libsonnet') {
  local tenantTemplate =
    template.new(
      'tenant',
      '$datasource',
      'query_result(sum by (id) (grafanacloud_logs_instance_info) and sum(label_replace(loki_tenant:active_streams{%s},"id","$1","tenant","(.*)")) by(id))' % selector(false).cluster().namespace().build(),
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
