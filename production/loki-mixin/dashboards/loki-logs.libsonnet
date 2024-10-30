local lokiLogs = (import './dashboard-loki-logs.json');
local template = import 'grafonnet/template.libsonnet';


(import 'dashboard-utils.libsonnet') {
  local deploymentTemplate =
    template.new(
      'deployment',
      '$datasource',
      'label_values(kube_deployment_created{' + $._config.per_cluster_label + '="$cluster", namespace="$namespace"}, deployment)',
      sort=1,
    ),

  local podTemplate =
    template.new(
      'pod',
      '$datasource',
      'label_values(kube_pod_container_info{' + $._config.per_cluster_label + '="$cluster", namespace="$namespace", pod=~"$deployment.*"}, pod)',
      sort=1,
    ),

  local containerTemplate =
    template.new(
      'container',
      '$datasource',
      'label_values(kube_pod_container_info{' + $._config.per_cluster_label + '="$cluster", namespace="$namespace", pod=~"$pod", pod=~"$deployment.*"}, container)',
      sort=1,
    ),

  local levelTemplate =
    template.custom(
      'level',
      'debug,info,warn,error',
      '',
      multi=true,
    ),

  local logTemplate =
    template.text(
      'filter',
      label='LogQL Filter'
    ),

  grafanaDashboards+: {
    local dashboards = self,

    'loki-logs.json': {
                        local cfg = self,

                        showMultiCluster:: true,

                      } + lokiLogs +
                      $.dashboard('Loki / Logs', uid='logs')
                      .addCluster()
                      .addNamespace()
                      .addTag()
                      .addLog() +
                      {
                        panels: [
                          p {
                            targets: [
                              e {
                                expr: if dashboards['loki-logs.json'].showMultiCluster
                                then std.strReplace(super.expr, 'cluster="$cluster"', $._config.per_cluster_label + '="$cluster"')
                                else std.strReplace(super.expr, 'cluster="$cluster", ', ''),
                              }
                              for e in p.targets
                            ],
                          }
                          for p in super.panels
                        ],
                        templating+: {
                          list+: [
                            deploymentTemplate,
                            podTemplate,
                            containerTemplate,
                            levelTemplate,
                            logTemplate,
                          ],
                        },
                      },
  },
}
