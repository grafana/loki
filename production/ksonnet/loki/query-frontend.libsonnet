{
  local container = $.core.v1.container,

  query_frontend_args::
    $._config.commonArgs {
      target: 'query-frontend',
      'log.level': 'debug',
    },

  query_frontend_container::
    container.new('query-frontend', $._images.query_frontend) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.query_frontend_args)) +
    $.jaeger_mixin +
    if $._config.queryFrontend.sharded_queries_enabled then
    $.util.resourcesRequests('2', '2Gi') +
    $.util.resourcesLimits(null, '6Gi') +
    container.withEnvMap({
      JAEGER_REPORTER_MAX_QUEUE_SIZE: '5000',
    })
    else $.util.resourcesRequests('2', '600Mi') +
    $.util.resourcesLimits(null, '1200Mi'),

  local deployment = $.apps.v1.deployment,

  query_frontend_deployment:
    deployment.new('query-frontend', $._config.queryFrontend.replicas, [$.query_frontend_container]) +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity,

  local service = $.core.v1.service,

  query_frontend_service:
    $.util.serviceFor($.query_frontend_deployment) +
    // Make sure that query frontend worker, running in the querier, do resolve
    // each query-frontend pod IP and NOT the service IP. To make it, we do NOT
    // use the service cluster IP so that when the service DNS is resolved it
    // returns the set of query-frontend IPs.
    service.mixin.spec.withClusterIp('None'),

}
