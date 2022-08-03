local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,

  query_frontend_args::
    $._config.commonArgs {
      target: 'query-frontend',
      'log.level': 'debug',
    },

  query_frontend_container::
    container.new('query-frontend', $._images.query_frontend) +
    container.withPorts($.util.grpclbDefaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.query_frontend_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    container.withEnvMixin($._config.commonEnvs) +
    $.jaeger_mixin +
    // sharded queries may need to do a nonzero amount of aggregation on the frontend.
    if $._config.queryFrontend.sharded_queries_enabled then
      k.util.resourcesRequests('2', '2Gi') +
      k.util.resourcesLimits(null, '6Gi')
    else k.util.resourcesRequests('2', '600Mi') +
         k.util.resourcesLimits(null, '1200Mi'),

  local deployment = k.apps.v1.deployment,

  query_frontend_deployment:
    deployment.new('query-frontend', $._config.queryFrontend.replicas, [$.query_frontend_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(5) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1),

  local service = k.core.v1.service,

  query_frontend_service:
    $.util.grpclbServiceFor($.query_frontend_deployment) +
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
