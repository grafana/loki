
local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,

  // Override frontend and querier configuration
  local override = {
      frontend: {
          scheduler_address: 'query-scheduler.%s.svc.cluster.local:9095' % $._config.namespace,
      },
      frontend_worker: {
          frontend_address: '',
          scheduler_address: 'query-scheduler.%s.svc.cluster.local:9095' % $._config.namespace,
      },
  },
  _config +: {
      loki: std.mergePatch($._config.loki, override),
  },

  query_scheduler_args::
    $._config.commonArgs {
      target: 'query-scheduler',
      'log.level': 'debug',
    },

  query_scheduler_container::
    container.new('query-scheduler', $._images.query_frontend) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.query_frontend_args)) +
    $.jaeger_mixin +
    // sharded queries may need to do a nonzero amount of aggregation on the frontend.
    if $._config.queryFrontend.sharded_queries_enabled then
      k.util.resourcesRequests('2', '2Gi') +
      k.util.resourcesLimits(null, '6Gi') +
      container.withEnvMap({
        JAEGER_REPORTER_MAX_QUEUE_SIZE: '5000',
      })
    else k.util.resourcesRequests('2', '600Mi') +
         k.util.resourcesLimits(null, '1200Mi'),

  local deployment = k.apps.v1.deployment,

  query_scheduler_deployment:
    deployment.new('query-scheduler', 1, [$.query_scheduler_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity,

  local service = k.core.v1.service,

  query_scheduler_service:
    k.util.serviceFor($.query_schduler_deployment)
}
