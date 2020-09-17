{
  local container = $.core.v1.container,

  querier_args::
    $._config.commonArgs {
      target: 'querier',
    },

  querier_container::
    container.new('querier', $._images.querier) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.querier_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    $.util.resourcesRequests($._config.querier.CPURequests, $._config.querier.memoryRequests),

  local deployment = $.apps.v1.deployment,

  querier_deployment:
    deployment.new('querier', $._config.querier.replicas, [$.querier_container]) +
    $.extra_tolerations +
    $.config_hash_mixin +
    $.extra_annotations +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity,

  querier_service:
    $.util.serviceFor($.querier_deployment),
}
