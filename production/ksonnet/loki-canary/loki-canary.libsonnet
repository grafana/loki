local config = import 'config.libsonnet';
local k = import 'ksonnet-util/kausal.libsonnet';

// backwards compatibility with ksonnet
local envVar = if std.objectHasAll(k.core.v1, 'envVar') then k.core.v1.envVar else k.core.v1.container.envType;

k + config {
  namespace: $.core.v1.namespace.new($._config.namespace),

  local container = $.core.v1.container,

  loki_canary_args:: {
    labelvalue: '$(POD_NAME)',
  },

  loki_canary_container::
    container.new('loki-canary', $._images.loki_canary) +
    $.util.resourcesRequests('10m', '20Mi') +
    container.withPorts($.core.v1.containerPort.new(name='http-metrics', port=80)) +
    container.withArgsMixin($.util.mapToFlags($.loki_canary_args)) +
    container.withEnv([
      envVar.fromFieldPath('HOSTNAME', 'spec.nodeName'),
      envVar.fromFieldPath('POD_NAME', 'metadata.name'),
    ]),

  local daemonSet = $.apps.v1.daemonSet,

  loki_canary_daemonset:
    daemonSet.new('loki-canary', [$.loki_canary_container]),
}
