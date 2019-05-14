local k = import 'ksonnet-util/kausal.libsonnet';

k {
  local container = $.core.v1.container,

  loki_canary_args:: {},

  _images+:: {
    loki_canary: 'loki-canary:latest',
  },

  loki_canary_container::
    container.new('loki-canary', $._images.loki_canary) +
    container.withPorts($.core.v1.containerPort.new('http-metrics', 80)) +
    container.withArgsMixin($.util.mapToFlags($.loki_canary_args)) +
    container.withEnv([
      container.envType.fromFieldPath('HOSTNAME', 'spec.nodeName'),
    ]),

  local daemonSet = $.extensions.v1beta1.daemonSet,

  local downwardApiMount(name, path, volumeMountMixin={}) =
        local container = $.core.v1.container,
              deployment = $.extensions.v1beta1.deployment,
              volumeMount = $.core.v1.volumeMount,
              volume = $.core.v1.volume,
              addMount(c) = c + container.withVolumeMountsMixin(
          volumeMount.new(name, path) +
          volumeMountMixin,
        );

        deployment.mapContainers(addMount) +
        deployment.mixin.spec.template.spec.withVolumesMixin([
          volume.withName(name) +
          volume.mixin.downwardApi.withItems([
            {
              path: "name",
              fieldRef: { fieldPath: "metadata.name" },
            },
          ]),
        ]),

  loki_canary_daemonset:
    daemonSet.new('loki-canary', [$.loki_canary_container]) +
    downwardApiMount('pod-name', '/etc/loki-canary'),
}