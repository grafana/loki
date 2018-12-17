{
  local container = $.core.v1.container,

  ingester_args::
    $._config.ringArgs {
      target: 'ingester',
      'ingester.num-tokens': '512',
      'ingester.join-after': '30s',
      'ingester.claim-on-rollout': true,
    },

  ingester_container::
    container.new('ingester', $._images.ingester) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.ingester_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort(80) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1),

  local deployment = $.apps.v1beta1.deployment,

  ingester_deployment:
    deployment.new('ingester', 3, [$.ingester_container]) +
    $.util.configVolumeMount('loki', '/etc/loki') +
    $.util.antiAffinity +
    deployment.mixin.spec.withMinReadySeconds(60) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(0) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1) +
    deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800),

  ingester_service:
    $.util.serviceFor($.ingester_deployment),
}
