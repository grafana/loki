local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';

local grafana = import 'grafana/grafana.libsonnet';
local envVar = if std.objectHasAll(k.core.v1, 'envVar') then k.core.v1.envVar else k.core.v1.container.envType;
local helm = tanka.helm.new(std.thisFile);

local spec = (import './spec.json').spec;

{
  local prometheusServerName = self.prometheus.service_prometheus_kube_prometheus_prometheus.metadata.name,
  local prometheusUrl = 'http://%s:9090' % prometheusServerName,

  local provisionedSecretPrefix = 'provisioned-secret',
  local adminTokenSecret = 'gel-admin-token',

  local tenant = 'loki',

  _config+:: {
    namespace: spec.namespace,
    adminTokenSecret: adminTokenSecret,
  },

  lokiNamespace: k.core.v1.namespace.new('loki'),
  gelLicenseSecret: k.core.v1.secret.new('gel-license', {}, type='Opaque')
                    + k.core.v1.secret.withStringData({
                      'license.jwt': importstr '../../secrets/gel.jwt',
                    })
                    + k.core.v1.secret.metadata.withNamespace('loki'),

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
  local lokiGatewayUrl = 'http://enterprise-logs-gateway.loki.svc.cluster.local',
  prometheus_datasource:: datasource.new('prometheus', prometheusUrl, type='prometheus', default=false),
  loki_datasource:: datasource.new('loki', lokiGatewayUrl, type='loki', default=true) +
                    datasource.withBasicAuth(tenant, '${PROVISIONED_TENANT_TOKEN}'),

  grafanaNamespace: k.core.v1.namespace.new('grafana'),
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
           + grafana.withEnterpriseLicenseText(importstr '../../secrets/grafana.jwt')
           + grafana.addDatasource('prometheus', $.prometheus_datasource)
           + grafana.addDatasource('loki', $.loki_datasource)
           + grafana.addPlugin('https://dl.grafana.com/gel/releases/grafana-enterprise-logs-app-v2.6.0.zip;grafana-enterprise-logs-app')
           + {
             local container = k.core.v1.container,
             grafana_deployment+:
               k.apps.v1.deployment.emptyVolumeMount('grafana-var', '/var/lib/grafana')
               + k.apps.v1.deployment.emptyVolumeMount('grafana-plugins', '/etc/grafana/provisioning/plugins')
               + k.apps.v1.deployment.spec.template.spec.withInitContainersMixin([
                 container.new('startup', 'alpine:latest') +
                 container.withCommand([
                   '/bin/sh',
                   '-euc',
                   |||
                     cat > /etc/grafana/provisioning/plugins/enterprise-logs.yaml <<EOF
                     apiVersion: 1
                     apps:
                       - type: grafana-enterprise-logs-app
                         jsonData:
                           backendUrl: %s
                           base64EncodedAccessTokenSet: true
                         secureJsonData:
                           base64EncodedAccessToken: "$$(echo -n ":$$GEL_ADMIN_TOKEN" | base64 | tr -d '[:space:]')"
                     EOF
                   ||| % lokiGatewayUrl,
                 ]) +
                 container.withVolumeMounts([
                   k.core.v1.volumeMount.new('grafana-var', '/var/lib/grafana', false),
                   k.core.v1.volumeMount.new('grafana-plugins', '/etc/grafana/provisioning/plugins', false),
                 ]) +
                 container.withImagePullPolicy('IfNotPresent') +
                 container.mixin.securityContext.withPrivileged(true) +
                 container.mixin.securityContext.withRunAsUser(0) +
                 container.mixin.withEnv([
                   envVar.fromSecretRef('GEL_ADMIN_TOKEN', adminTokenSecret, 'token'),
                 ]),
               ]) + k.apps.v1.deployment.mapContainers(
                 function(c) c {
                   env+: [
                     envVar.fromSecretRef('PROVISIONED_TENANT_TOKEN', '%s-%s' % [provisionedSecretPrefix, tenant], 'password'),
                   ],
                 }
               ),
           },
}
