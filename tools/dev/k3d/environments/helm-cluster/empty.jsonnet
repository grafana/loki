local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';
local configMap = k.core.v1.configMap;

local spec = (import './spec.json').spec;

{
  _config+:: {
    namespace: spec.namespace,
  },

  lokiNamespace: k.core.v1.namespace.new('loki'),

  gelLicenseSecret: k.core.v1.secret.new('gel-license', {}, type='Opaque')
                    + k.core.v1.secret.withStringData({
                      'license.jwt': importstr '../../secrets/gel.jwt',
                    })
                    + k.core.v1.secret.metadata.withNamespace('loki'),
  local grafanaCloudCredentials = import '../../secrets/grafana-cloud-credentials.json',
  grafanaCloudMetricsCredentials: k.core.v1.secret.new('grafana-cloud-metrics-credentials', {}, type='Opaque')
                                  + k.core.v1.secret.withStringData({
                                    username: '%d' % grafanaCloudCredentials.metrics.username,
                                    password: grafanaCloudCredentials.metrics.password,
                                  })
                                  + k.core.v1.secret.metadata.withNamespace('loki'),
  grafanaCloudLogsCredentials: k.core.v1.secret.new('grafana-cloud-logs-credentials', {}, type='Opaque')
                               + k.core.v1.secret.withStringData({
                                 username: '%d' % grafanaCloudCredentials.logs.username,
                                 password: grafanaCloudCredentials.logs.password,
                               })
                               + k.core.v1.secret.metadata.withNamespace('loki'),


}
