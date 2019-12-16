{
  local container = $.core.v1.container,

  query_frontend_args:: {
    target: 'query-frontend',
  },

  query_frontend_container::
    container.new('query-frontend', $._images.query_frontend) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.query_frontend_args)) +
    $.util.resourcesRequests('2', '600Mi') +
    $.util.resourcesLimits(null, '1200Mi') +
    $.jaeger_mixin,

  local deployment = $.apps.v1beta1.deployment,

  query_frontend_deployment:
    deployment.new('query-frontend', 2, [$.query_frontend_container]) +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki') +
    $.util.antiAffinity,

  local service = $.core.v1.service,

  query_frontend_service:
    $.util.serviceFor($.query_frontend_deployment) +
    service.mixin.spec.withClusterIp('None'),
}
