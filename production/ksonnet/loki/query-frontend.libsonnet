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
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    $.jaeger_mixin +
    if $._config.queryFrontend.sharded_queries_enabled then
      $.util.resourcesRequests($._config.queryFrontend.CPURequests, $._config.queryFrontend.memoryRequests) +
      $.util.resourcesLimits(null, $._config.queryFrontend.memoryLimits) +
      container.withEnvMap({
        JAEGER_REPORTER_MAX_QUEUE_SIZE: '5000',
      })
    else $.util.resourcesRequests($._config.queryFrontend.CPURequests, $._config.queryFrontend.memoryRequestsSharded) +
         $.util.resourcesLimits(null, $._config.queryFrontend.memoryLimitsSharded),

  local deployment = $.apps.v1.deployment,

  query_frontend_deployment:
    deployment.new('query-frontend', $._config.queryFrontend.replicas, [$.query_frontend_container]) +
    $.config_hash_mixin +
    $.extra_annotations +
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
    service.mixin.spec.withClusterIp('None') +
    // Query frontend will not become ready until at least one querier connects
    // which creates a chicken and egg scenario if we don't publish the
    // query-frontend address before it's ready.
    service.mixin.spec.withPublishNotReadyAddresses(true),

}
