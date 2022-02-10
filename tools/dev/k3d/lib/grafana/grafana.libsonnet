local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';
local spec = (import './spec.json').spec;
local helm = tanka.helm.new(std.thisFile) {
  template(name, chart, conf={})::
    std.native('helmTemplate')(name, chart, conf { calledFrom: std.thisFile }),
};
{
  _config+:: {
    jaegerAgentName: error 'please provide $._config.jaegerAgentName',
    jaegerAgentPort: 6831,
    namespace: error 'plase provide $._config.namespace',
    provisioningDir: '/etc/grafana/provisioning',
    lokiUrl: error 'please provide $._config.lokiUrl',
    grafana: {
      datasources: [],
    },
  },

  _images+:: {
    grafana: {
      repository: 'grafana/grafana',
      tag: 'latest',
      pullPolicy: 'IfNotPresent',
    },
  },

  grafana: helm.template('grafana', '../../charts/grafana', {
    namespace: $._config.namespace,
    values: {
      image: $._images.grafana,
      testFramework: {
        enabled: false,
      },
      env: {
        GF_AUTH_ANONYMOUS_ENABLED: true,
        GF_AUTH_ANONYMOUS_ORG_ROLE: 'Admin',
        GF_FEATURE_TOGGLES_ENABLE: 'ngalert',
        JAEGER_AGENT_PORT: 6831,
        JAEGER_AGENT_HOST: $._config.jaegerAgentName,
      },
      podAnnotations: {
        'prometheus.io/scrape': 'true',
        'prometheus.io/port': '3000',
      },
      datasources: {
        'datasources.yaml': {
          apiVersion: 1,
          datasources: $._config.grafana.datasources,
        },
      },
      'grafana.ini': {
        'tracing.jaeger': {
          always_included_tag: 'app=grafana',
          sampler_type: 'const',
          sampler_param: 1,
        },
        paths: {
          provisioning: $._config.provisioningDir,
        },
      },
    },
    kubeVersion: 'v1.18.0',
    noHooks: false,
  }),
}
