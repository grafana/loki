local k = import 'ksonnet-util/kausal.libsonnet';

{
  local max_outstanding = if $._config.queryFrontend.sharded_queries_enabled then 1024 else 256,

  // Override frontend and querier configuration
  _config+:: {
    loki+: if $._config.query_scheduler_enabled then {
      frontend+: {
        scheduler_address: 'query-scheduler-discovery.%s.svc.cluster.local:9095' % $._config.namespace,
      },
      frontend_worker+: {
        scheduler_address: 'query-scheduler-discovery.%s.svc.cluster.local:9095' % $._config.namespace,
      },
      query_scheduler+: {
        max_outstanding_requests_per_tenant: max_outstanding,
      },
    } else {
      frontend+: {
        max_outstanding_per_tenant: max_outstanding,
      },
      frontend_worker+: {
        frontend_address: 'query-frontend.%s.svc.cluster.local:9095' % $._config.namespace,
      },
    },
  },

  query_scheduler_args:: if $._config.query_scheduler_enabled then
    $._config.commonArgs {
      target: 'query-scheduler',
      'log.level': 'debug',
    }
  else {},

  local container = k.core.v1.container,
  query_scheduler_container:: if $._config.query_scheduler_enabled then
    container.new('query-scheduler', $._images.query_scheduler) +
    container.withPorts($.util.grpclbDefaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.query_scheduler_args)) +
    $.jaeger_mixin +
    k.util.resourcesRequests('2', '600Mi') +
    k.util.resourcesLimits(null, '1200Mi')
  else {},

  local deployment = k.apps.v1.deployment,
  query_scheduler_deployment: if $._config.query_scheduler_enabled then
    deployment.new('query-scheduler', 2, [$.query_scheduler_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(5) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1)
  else {},

  local service = k.core.v1.service,

  // Headless to make sure resolution gets IP address of target pods, and not service IP.
  query_scheduler_discovery_service: if !$._config.query_scheduler_enabled then {} else
    $.util.grpclbServiceFor($.query_scheduler_deployment) +
    service.mixin.spec.withPublishNotReadyAddresses(true) +
    service.mixin.spec.withClusterIp('None') +
    service.mixin.metadata.withName('query-scheduler-discovery'),
}
