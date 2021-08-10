
local k = import 'ksonnet-util/kausal.libsonnet';

{
  // Override frontend and querier configuration
  _config +:: {
    loki+: if $._config.query_scheduler_enabled then {
      frontend+: {
        scheduler_address: 'query-scheduler.%s.svc.cluster.local:9095' % $._config.namespace,
      },
      frontend_worker+: {
        frontend_address: '',
        scheduler_address: 'query-scheduler.%s.svc.cluster.local:9095' % $._config.namespace,
      },
    } else {},
  },

  query_scheduler_args:: if $._config.query_scheduler_enabled then
    $._config.commonArgs {
      target: 'query-scheduler',
      'log.level': 'debug',
    }
  else {},

  local container = k.core.v1.container,
  query_scheduler_container:: if $._config.query_scheduler_enabled then
    container.new('query-scheduler', $._images.query_frontend) +
    container.withPorts($.util.defaultPorts) +
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
    k.util.antiAffinity
  else {},

  local service = k.core.v1.service,
  query_scheduler_service: if $._config.query_scheduler_enabled then
    k.util.serviceFor($.query_scheduler_deployment)
  else {},
}
