local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';
local helm = tanka.helm.new(std.thisFile) {
  template(name, chart, conf={})::
    std.native('helmTemplate')(name, chart, conf { calledFrom: std.thisFile }),
};
{
  local envVar = k.core.v1.envVar,
  _config+:: {
    jaegerAgentName: error 'please provide $._config.jaegerAgentName',
    jaegerAgentPort: 6831,
    namespace: error 'plase prvoide $._config.namespace',
  },

  _prometheusAnnotations:: { 'prometheus.io/scrape': 'true', 'prometheus.io/port': '3100' },

  prometheus: helm.template('prometheus', '../../charts/prometheus', {
    namespace: $._config.namespace,
    values: {
      server: {
        env: [
          envVar.new('JAEGER_AGENT_HOST', $._config.jaegerAgentName),
          envVar.new('JAEGER_AGENT_PORT', '%d' % $._config.jaegerAgentPort),
          envVar.new('JAEGER_SAMPLER_TYPE', 'const'),
          envVar.new('JAEGER_SAMPLER_PARAM', '1'),
          envVar.new('JAEGER_TAGS', 'app=prometheus'),
        ],
      },
      alertmanager: {
        enabled: false,
      },
      pushgateway: {
        enabled: false,
      },
    },
    kubeVersion: 'v1.18.0',
    noHooks: false,
  }),

}
