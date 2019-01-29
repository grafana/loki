{
  local container = $.core.v1.container,

  ingester_args::
    $._config.commonArgs {
      target: 'ingester',
    },

  ingester_container::
    container.new('ingester', $._images.ingester) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.ingester_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort(80) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    $.util.resourcesRequests('1', '5Gi') +
    $.util.resourcesLimits('2', '10Gi'),

  local deployment = $.apps.v1beta1.deployment,

  ingester_deployment:
    deployment.new('ingester', 3, [$.ingester_container]) +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki') +
    $.util.antiAffinity +
    deployment.mixin.spec.withMinReadySeconds(60) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(0) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1) +
    deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800),

  ingester_service:
    $.util.serviceFor($.ingester_deployment),
}
