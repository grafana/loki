local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';
local spec = (import './spec.json').spec;

local jaeger = import 'jaeger/jaeger.libsonnet';
local grafana = import 'grafana/grafana.libsonnet';
local prometheus = import 'prometheus/prometheus.libsonnet';
local promtail = import 'promtail/promtail.libsonnet';

local helm = tanka.helm.new(std.thisFile) {
  template(name, chart, conf={})::
    std.native('helmTemplate')(name, chart, conf { calledFrom: std.thisFile }),
};
local clusterName = 'loki-distributed';
local normalizedClusterName = std.strReplace(clusterName, '-', '_');

grafana + prometheus + promtail + jaeger {
  local gatewayName = self.loki.service_loki_distributed_gateway.metadata.name,
  local gatewayHost = '%s' % gatewayName,
  local gatewayUrl = 'http://%s' % gatewayHost,
  local jaegerQueryName = self.jaeger.query_service.metadata.name,
  local jaegerQueryUrl = 'http://%s' % jaegerQueryName,
  local jaegerAgentName = self.jaeger.agent_service.metadata.name,
  local jaegerAgentUrl = 'http://%s' % jaegerAgentName,
  local prometheusServerName = self.prometheus.service_prometheus_server.metadata.name,
  local prometheusUrl = 'http://%s' % prometheusServerName,
  local namespace = spec.namespace,

  _config+:: {
    clusterName: clusterName,
    gatewayName: gatewayName,
    gatewayHost: gatewayHost,
    gelUrl: gatewayUrl,
    jaegerAgentName: jaegerAgentName,
    jaegerAgentPort: 6831,
    namespace: namespace,
    adminToken: 'gel-admin-token',

    grafana: {
      datasources: [
        {
          name: 'Prometheus',
          type: 'prometheus',
          access: 'proxy',
          url: prometheusUrl,
        },
        {
          name: 'Jaeger',
          type: 'jaeger',
          access: 'proxy',
          url: jaegerQueryUrl,
          uid: 'jaeger_uid',
        },
        {
          name: 'Loki',
          type: 'loki',
          access: 'proxy',
          url: gatewayUrl,
          jsonData: {
            derivedFields: [
              {
                datasourceUid: 'jaeger_uid',
                matcherRegex: 'traceID=(\\w+)',
                name: 'TraceID',
                url: '$${__value.raw}',
              },
            ],
          },
        },
      ],
    },
  },

  minio: helm.template('minio', '../../charts/minio', {
    namespace: $._config.namespace,
    values: {
      accessKey: 'loki',
      rootUser: 'loki',
      secretKey: 'supersecret',
      rootPassword: 'supersecret',
      buckets: [
        {
          name: 'loki-data',
          policy: 'public',
          purge: false,
        },
      ],
      persistence: {
        enabled: true,
        'storage-class': 'local-path',
        size: '10Gi',
      },
    },
  }),

  local config = import './values/default/config.libsonnet',
  local values = (import './values/default/values.libsonnet').lokiValues(k.util.manifestYaml(config)),

  loki: helm.template($._config.clusterName, '../../charts/loki-distributed', {
    namespace: $._config.namespace,
    values: values {
      local registry = 'k3d-grafana:45629',
      loki+: {
        image: {
          registry: registry,
          repository: 'loki',
          tag: 'latest',
          pullPolicy: 'Always',
        },
      },
    },
  }) + {
    ['deployment_loki_distributed_%s' % [name]]+:
      k.apps.v1.deployment.mapContainers($._addJaegerEnvVars) +
      k.apps.v1.deployment.spec.template.metadata.withAnnotations($._prometheusAnnotations)
    for name in [
      'compactor',
      'distributor',
      'gateway',
      'query_frontend',
    ]
  } + {
    ['stateful_set_loki_distributed_%s' % [name]]+:
      k.apps.v1.statefulSet.mapContainers($._addJaegerEnvVars) +
      k.apps.v1.statefulSet.spec.template.metadata.withAnnotations($._prometheusAnnotations)
    for name in [
      'ingester',
      'querier',
    ]
  },
}
