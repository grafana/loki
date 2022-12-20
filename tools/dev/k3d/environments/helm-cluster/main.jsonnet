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
           + {
             local configMap = k.core.v1.configMap,
             [config]+:
               configMap.metadata.withNamespace('loki')
             for config in [
               'grafana_ini_config_map',
               'grafana_datasource_config_map',
               'notification_channel_config_map',
               'dashboard_provisioning_config_map',
             ]
           }
           + {
             grafana_service+: k.core.v1.service.metadata.withNamespace('loki'),

             local container = k.core.v1.container,
             grafana_deployment+:
               k.apps.v1.deployment.metadata.withNamespace('loki')
               + k.apps.v1.deployment.hostVolumeMount(
                 name='enterprise-logs-app',
                 hostPath='/var/lib/grafana/plugins/grafana-enterprise-logs-app/dist',
                 path='/grafana-enterprise-logs-app',
                 volumeMixin=k.core.v1.volume.hostPath.withType('Directory')
               )
               + k.apps.v1.deployment.emptyVolumeMount('grafana-var', '/var/lib/grafana')
               + k.apps.v1.deployment.emptyVolumeMount('grafana-plugins', '/etc/grafana/provisioning/plugins')
               + k.apps.v1.deployment.spec.template.spec.withInitContainersMixin([
                 container.new('startup', 'alpine:latest') +
                 container.withCommand([
                   '/bin/sh',
                   '-euc',
                   |||
                     mkdir -p /var/lib/grafana/plugins
                     cp -r /grafana-enterprise-logs-app /var/lib/grafana/plugins/grafana-enterprise-logs-app
                     chown -R 472:472 /var/lib/grafana/plugins

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
                   k.core.v1.volumeMount.new('enterprise-logs-app', '/grafana-enterprise-logs-app', false),
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
                     envVar.new('GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS', 'grafana-enterprise-logs-app'),
                     envVar.fromSecretRef('PROVISIONED_TENANT_TOKEN', '%s-%s' % [provisionedSecretPrefix, tenant], 'token-read'),
                   ],
                 }
               ),
           },

  minioNamespace: k.core.v1.namespace.new('minio'),
  minio: helm.template('minio', '../../charts/minio', {
    namespace: 'minio',
    values: {
      replicas: 1,
      // Minio requires 2 to 16 drives for erasure code (drivesPerNode * replicas)
      // https://docs.min.io/docs/minio-erasure-code-quickstart-guide
      // Since we only have 1 replica, that means 2 drives must be used.
      drivesPerNode: 2,
      rootUser: 'loki',
      rootPassword: 'supersecret',
      buckets: [
        {
          name: 'chunks',
          policy: 'none',
          purge: false,
        },
        {
          name: 'ruler',
          policy: 'none',
          purge: false,
        },
        {
          name: 'admin',
          policy: 'none',
          purge: false,
        },
      ],
      persistence: {
        size: '5Gi',
      },
      resources: {
        requests: {
          cpu: '100m',
          memory: '128Mi',
        },
      },
    },
  }),
}
