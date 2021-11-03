local lokiLogs = (import './dashboard-loki-logs.json');
local template = import 'grafonnet/template.libsonnet';


{
  local deploymentTemplate =
    template.new(
      'deployment',
      '$datasource',
      'label_values(kube_deployment_created{cluster="$cluster", namespace="$namespace"}, deployment)',
      sort=1,
    ),

  local podTemplate =
    template.new(
      'pod',
      '$datasource',
      'label_values(kube_pod_container_info{cluster="$cluster", namespace="$namespace", pod=~"$deployment.*"}, pod)',
      sort=1,
    ),

  local containerTemplate =
    template.new(
      'container',
      '$datasource',
      'label_values(kube_pod_container_info{cluster="$cluster", namespace="$namespace", pod=~"$pod", pod=~"$deployment.*"}, container)',
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
      clusterLabel:: 'cluster',

    } + lokiLogs +
    $.dashboard('Loki / Logs')
      // TODO (callum) For this cluster the cluster template is not actually
      // added since the json defines the template array, we just need the tags
      // and links from this function until they're moved to a separate function.
      .addClusterSelectorTemplates(false)
      .addLog() +
    {
      panels: [
        p + {
          targets: [
            e + {
              expr: if dashboards['loki-logs.json'].showMultiCluster then super.expr
              else std.strReplace(super.expr, 'cluster="$cluster", ', '')
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
