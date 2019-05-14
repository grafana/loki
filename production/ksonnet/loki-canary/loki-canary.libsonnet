local k = import 'ksonnet-util/kausal.libsonnet';

k {
  local container = $.core.v1.container,

  loki_canary_args:: {
    labelvalue: "$(POD_NAME)",
  },

  _images+:: {
    loki_canary: 'grafana/loki-canary:latest',
  },

  loki_canary_container::
    container.new('loki-canary', $._images.loki_canary) +
    container.withPorts($.core.v1.containerPort.new('http-metrics', 80)) +
    container.withArgsMixin($.util.mapToFlags($.loki_canary_args)) +
    container.withEnv([
      container.envType.fromFieldPath('HOSTNAME', 'spec.nodeName'),
      container.envType.fromFieldPath('POD_NAME', 'metadata.name'),
    ]),

  local daemonSet = $.extensions.v1beta1.daemonSet,

  loki_canary_daemonset:
    daemonSet.new('loki-canary', [$.loki_canary_container]),
}