local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,

  table_manager_args::
    $._config.commonArgs {
      target: 'table-manager',

      // Rate limit Bigtable Admin calls.  Google seem to limit to ~100QPS,
      // and given 2yrs worth of tables (~100) a sync will table 20s.  This
      // allows you to run upto 20 independent Cortex clusters on the same
      // Google project before running into issues.
      'bigtable.grpc-client-rate-limit': 5.0,
      'bigtable.grpc-client-rate-limit-burst': 5,
      'bigtable.backoff-on-ratelimits': true,
      'bigtable.table-cache.enabled': true,
    },

  table_manager_container::
    container.new('table-manager', $._images.tableManager) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.table_manager_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    container.withEnvMixin($._config.commonEnvs) +
    k.util.resourcesRequests('100m', '100Mi') +
    k.util.resourcesLimits('200m', '200Mi'),

  local deployment = k.apps.v1.deployment,

  table_manager_deployment:
    deployment.new('table-manager', 1, [$.table_manager_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config'),

  table_manager_service:
    k.util.serviceFor($.table_manager_deployment, $._config.service_ignored_labels),
}
