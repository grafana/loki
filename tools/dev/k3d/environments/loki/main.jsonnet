local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';

local grafana = import 'grafana/grafana.libsonnet';
local envVar = if std.objectHasAll(k.core.v1, 'envVar') then k.core.v1.envVar else k.core.v1.container.envType;
local helm = tanka.helm.new(std.thisFile);

local spec = (import './spec.json').spec;

{
  local prometheusServerName = self.prometheus.service_prometheus_kube_prometheus_prometheus.metadata.name,
  local prometheusUrl = 'http://%s:9090' % prometheusServerName,

  local lokiGatewayHost = self.loki.service_loki_gateway.metadata.name,
  local lokiGatewayUrl = 'http://%s' % lokiGatewayHost,

  local tenant = 'loki',

  _config+:: {
    clusterName: 'loki',
    namespace: spec.namespace,
    adminApiUrl: lokiGatewayUrl,
  },

  loki: helm.template($._config.clusterName, '../../../../../production/helm/loki', {
    namespace: $._config.namespace,
    values: {
      loki+: {
        image: {
          repository: 'grafana/loki',
          tag: 'main-f5fbfab-amd64',
        },
      },
      enterprise+: {
        enabled: false,
      },
      monitoring+: {
        selfMonitoring+: {
          tenant: tenant,
        },
        serviceMonitor: {
          //TODO: this is required because of the service monitor selector match labels
          // from kube-prometheus-stack.
          labels: { release: 'prometheus' },
        },
      },
      minio+: {
        enabled: true,
      },
    },
  }),

  prometheus: helm.template('prometheus', '../../charts/kube-prometheus-stack', {
    namespace: $._config.namespace,
    values+: {
      grafana+: {
        enabled: false,
      },
      prometheus: {
        prometheusSpec: {
          serviceMonitorSelector: {
            matchLabels: {
              release: 'prometheus',
            },
          },
        },
      },
    },
    kubeVersion: 'v1.18.0',
    noHooks: false,
  }),

  local datasource = grafana.datasource,
  prometheus_datasource:: datasource.new('prometheus', prometheusUrl, type='prometheus', default=false),
  loki_datasource:: datasource.new('loki', lokiGatewayUrl, type='loki', default=true) +
                    datasource.withJsonData({ httpHeaderName1: 'X-Scope-OrgID' }) +
                    datasource.withSecureJsonData({ httpHeaderValue1: tenant }),

  grafana: grafana
           + grafana.withAnonymous()
           + grafana.withImage('grafana/grafana-enterprise:8.2.5')
           + grafana.withGrafanaIniConfig({
             sections+: {
               server+: {
                 http_port: 3000,
               },
               users+: {
                 default_theme: 'light',
               },
               paths+: {
                 provisioning: '/etc/grafana/provisioning',
               },
             },
           })
           + grafana.addDatasource('prometheus', $.prometheus_datasource)
           + grafana.addDatasource('loki', $.loki_datasource),
}
