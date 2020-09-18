{
  local container = $.core.v1.container,
  local containerPort = $.core.v1.containerPort,

  distributor_args::
    $._config.commonArgs {
      target: 'distributor',
    },

  distributor_container::
    container.new('distributor', $._images.distributor) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.distributor_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    $.util.resourcesRequests($._config.distributor.cpuRequests, $._config.distributor.memoryRequests) +
    $.util.resourcesLimits($._config.distributor.cpuLimits, $._config.distributor.memoryLimits),

  local deployment = $.apps.v1.deployment,

  distributor_deployment:
    deployment.new('distributor', $._config.distributor.replicas, [$.distributor_container]) +
    deployment.spec.template.spec.withTolerations($._config.tolerations) +
    $.config_hash_mixin +
    deployment.mixin.spec.template.metadata.withLabelsMixin($._config.labels) +
    deployment.mixin.spec.template.metadata.withAnnotationsMixin($._config.annotations) +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity,

  distributor_service:
    $.util.serviceFor($.distributor_deployment),
}
