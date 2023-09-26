local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';
local container = k.core.v1.container;
local configMap = k.core.v1.configMap;
local deployment = k.apps.v1.deployment;
local volume = k.core.v1.volume;

local grafana = import 'grafana/grafana.libsonnet';
local envVar = if std.objectHasAll(k.core.v1, 'envVar') then k.core.v1.envVar else k.core.v1.container.envType;
local helm = tanka.helm.new(std.thisFile);

local spec = (import './spec.json').spec;

local enterprise = std.extVar('enterprise');
local clusterName = if enterprise then 'enterprise-logs-test-fixture' else 'loki';
local lokiGatewayUrl = if enterprise then
  'http://enterprise-logs-gateway.loki.svc.cluster.local'
else 'http://loki-gateway.loki.svc.cluster.local';

local tenant = 'loki';

{
  local prometheusServerName = self.prometheus.service_prometheus_kube_prometheus_prometheus.metadata.name,
  local prometheusUrl = 'http://%s:9090' % prometheusServerName,

  _config+:: {
    namespace: spec.namespace,
  },

  lokiNamespace: k.core.v1.namespace.new('loki'),
  prometheus: helm.template('prometheus', '../../charts/kube-prometheus-stack', {
    local clusterRelabel =
      {
        action: 'replace',
        replacement: clusterName,
        targetLabel: 'cluster',
      },
    namespace: $._config.namespace,
    values+: {
      grafana+: {
        enabled: false,
      },

      // the CRDs are failing to be created due to an annotation being too many characters
      // they shouldn't be needed as they are already installed in the create_cluster.sh script
      crds+: {
        enabled: false,
      },

      kubelet: {
        serviceMonitor: {
          cAdvisorRelabelings: [
            clusterRelabel,
            {
              targetLabel: 'metrics_path',
              sourceLabels: [
                '__metrics_path__',
              ],
            },
            {
              targetLabel: 'instance',
              sourceLabels: [
                'node',
              ],
            },
          ],
        },
      },

      defaultRules: {
        additionalRuleLabels: {
          cluster: clusterName,
        },
      },

      'kube-state-metrics': {
        prometheus: {
          monitor: {
            relabelings: [
              clusterRelabel,
              {
                targetLabel: 'instance',
                sourceLabels: [
                  '__meta_kubernetes_pod_node_name',
                ],
              },
            ],
          },
        },
      },

      'prometheus-node-exporter': {
        prometheus: {
          monitor: {
            relabelings: [
              clusterRelabel,
              {
                targetLabel: 'instance',
                sourceLabels: [
                  '__meta_kubernetes_pod_node_name',
                ],
              },
            ],
          },
        },
      },

      prometheus: {
        prometheusSpec: {
          serviceMonitorSelector: {
            matchLabels: {
              release: 'prometheus',
            },
          },
        },
        monitor: {
          relabelings: [
            clusterRelabel,
          ],
        },
      },
    },
    kubeVersion: 'v1.18.0',
    noHooks: false,
  }),

  local datasource = grafana.datasource,

  prometheus_datasource:: datasource.new('prometheus', prometheusUrl, type='prometheus', default=false),
  loki_datasource:: datasource.new('loki', lokiGatewayUrl, type='loki', default=true) +
                    if enterprise then datasource.withBasicAuth(tenant, '${PROVISIONED_TENANT_TOKEN}') else
                      datasource.withJsonData({
                        httpHeaderName1: 'X-Scope-OrgID',
                      }) + datasource.withSecureJsonData({
                        httpHeaderValue1: tenant,
                      }),

  grafanaNamespace: k.core.v1.namespace.new('grafana'),

  local dashboardsPrefix = if enterprise then 'enterprise-logs' else 'loki',
  local grafanaImage = if enterprise then
    grafana.withImage('grafana/grafana-enterprise:9.5.1') else
    grafana.withImage('grafana/grafana:9.5.1'),
  grafana+: grafana
            + grafana.withAnonymous()
            + grafanaImage
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
            + grafana.addDatasource('loki', $.loki_datasource) + {
    grafana_deployment+:
      deployment.emptyVolumeMount('grafana-var', '/var/lib/grafana')
      + deployment.emptyVolumeMount('grafana-plugins', '/etc/grafana/provisioning/plugins')
      + deployment.configVolumeMount(
        '%s-dashboards-1' % dashboardsPrefix,
        '/var/lib/grafana/dashboards/loki-1',
        {},
        volume.configMap.withOptional(true)  //no dashboards for single binary mode
      )
      + deployment.configVolumeMount(
        '%s-dashboards-2' % dashboardsPrefix,
        '/var/lib/grafana/dashboards/loki-2',
        {},
        volume.configMap.withOptional(true)  //no dashboards for single binary mode
      ),

    dashboard_provisioning_config_map:
      configMap.new('grafana-dashboard-provisioning') +
      configMap.withData({
        'dashboards.yml': k.util.manifestYaml({
          apiVersion: 1,
          providers: [
            {
              name: 'loki-1',
              orgId: 1,
              folder: 'Loki',
              type: 'file',
              disableDeletion: true,
              editable: false,
              options: {
                path: '/var/lib/grafana/dashboards/loki-1',
              },
            },
            {
              name: 'loki-2',
              orgId: 1,
              folder: 'Loki',
              type: 'file',
              disableDeletion: true,
              editable: false,
              options: {
                path: '/var/lib/grafana/dashboards/loki-2',
              },
            },
          ],
        }),
      }),
  },
} + if enterprise then
  {
    local adminTokenSecret = 'gel-admin-token',
    local provisionedSecretPrefix = 'provisioned-secret',

    _config+:: {
      adminTokenSecret: adminTokenSecret,
    },

    gelLicenseSecret: k.core.v1.secret.new('gel-license', {}, type='Opaque')
                      + k.core.v1.secret.withStringData({
                        'license.jwt': importstr '../../secrets/gel.jwt',
                      })
                      + k.core.v1.secret.metadata.withNamespace('loki'),
    grafana+: grafana.withEnterpriseLicenseText(importstr '../../secrets/grafana.jwt')
              + grafana.addPlugin(
                'https://storage.googleapis.com/grafana-enterprise-logs/dev/grafana-enterprise-logs-app-9515528.zip;grafana-enterprise-logs-app'
              ) + {
      grafana_deployment+:
        k.apps.v1.deployment.spec.template.spec.withInitContainersMixin([
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
        ])
        + k.apps.v1.deployment.mapContainers(
          function(c) c {
            env+: [
              envVar.fromSecretRef('PROVISIONED_TENANT_TOKEN', '%s-%s' % [provisionedSecretPrefix, tenant], 'password'),
            ],
          }
        ),
    },
  } else {}
