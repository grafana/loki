local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
local tanka = import 'github.com/grafana/jsonnet-libs/tanka-util/main.libsonnet';
local helm = tanka.helm.new(std.thisFile) {
  template(name, chart, conf={})::
    std.native('helmTemplate')(name, chart, conf { calledFrom: std.thisFile }),
};
{
  _config+:: {
    namespace: error 'please provide $._config.namespace',
    gatewayHost: error 'please provide $._config.gatewayAddress',
  },

  promtail: helm.template('promtail', '../../charts/promtail', {
    namespace: $._config.namespace,
    values: {
      extraArgs: ['--config.expand-env=true'],
      config: {
        lokiAddress: 'http://%s/loki/api/v1/push' % $._config.gatewayHost,
      },
    },
    kubeVersion: 'v1.18.0',
    noHooks: false,
  }),
}
