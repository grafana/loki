{
  local container = $.core.v1.container,
  local pvc = $.core.v1.persistentVolumeClaim,
  local volumeMount = $.core.v1.volumeMount,
  local statefulSet = $.apps.v1.statefulSet,

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
    $.util.resourcesRequests(
      $._config.querier_resources_requests_cpu,
      $._config.querier_resources_requests_memory) +
    $.util.resourcesLimits(
      $._config.querier_resources_limits_cpu,
      $._config.querier_resources_limits_memory) +
    if $._config.querier_stateful then
      container.withVolumeMountsMixin([
        volumeMount.new('querier-data', '/data'),
      ]) else {},

  local deployment = $.apps.v1.deployment,

  querier_deployment: if !$._config.querier_stateful then
    deployment.new('querier', $._config.querier_replicas, [$.querier_container]) +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity
    else {},

  // PVC for queriers when running as statefulsets
  querier_data_pvc:: if $._config.querier_stateful then
    pvc.new('querier-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.querier_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName('fast')
    else {},

  querier_statefulset: if $._config.querier_stateful then
    statefulSet.new('querier', $._config.querier_replicas, [$.querier_container], $.querier_data_pvc) +
    statefulSet.mixin.spec.withServiceName('querier') +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001)  // 10001 is the group ID assigned to Loki in the Dockerfile
    else {},

  querier_service:
    if !$._config.querier_stateful then
      $.util.serviceFor($.querier_deployment)
    else
      $.util.serviceFor($.querier_statefulset),
}
