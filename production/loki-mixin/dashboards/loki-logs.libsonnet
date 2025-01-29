local lokiLogs = (import './dashboard-loki-logs.json');
local template = import 'grafonnet/template.libsonnet';


local selector = (import '../selectors.libsonnet').new;
local templateSelector = selector(false).cluster().namespace();

local deploymentTemplate =
  template.new(
    'deployment',
    '$datasource',
    'label_values(kube_deployment_created{%s}, deployment)' % templateSelector.build(),
    sort=1,
  );

local podTemplate =
  template.new(
    'pod',
    '$datasource',
    'label_values(kube_pod_container_info{%s}, pod)' % templateSelector.label('pod').re('$deployment.*').build(),
    sort=1,
  );

local containerTemplate =
  template.new(
    'container',
    '$datasource',
    'label_values(kube_pod_container_info{%s}, container)' % templateSelector.label('pod').re('$pod').build(),
    sort=1,
  );

local levelTemplate =
  template.custom(
    'level',
    'debug,info,warn,error',
    '',
    multi=true,
  );

local logTemplate =
  template.text(
    'filter',
    label='LogQL Filter'
  );

(import 'dashboard-utils.libsonnet') {

  grafanaDashboards+: {
    local dashboards = self,

    'loki-logs.json': {
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
