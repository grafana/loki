{
  local container = $.core.v1.container,

  table_manager_args::
    $._config.commonArgs {
      target: 'table-manager',
    },

  table_manager_container::
    container.new('table-manager', $._images.tableManager) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.table_manager_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    $.util.resourcesRequests('100m', '100Mi') +
    $.util.resourcesLimits('200m', '200Mi'),

  local deployment = $.apps.v1.deployment,

  table_manager_deployment:
    deployment.new('table-manager', 1, [$.table_manager_container]) +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki/config'),

  table_manager_service:
    $.util.serviceFor($.table_manager_deployment),
}
