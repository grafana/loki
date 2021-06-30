local k = import 'ksonnet-util/kausal.libsonnet';
{
  local container = k.core.v1.container,
  local pvc = k.core.v1.persistentVolumeClaim,
  local volumeMount = k.core.v1.volumeMount,
  local statefulSet = k.apps.v1.statefulSet,

  querier_args::
    $._config.commonArgs {
      target: 'querier',
    },

  querier_container::
    container.new('querier', $._images.querier) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.querier_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('4', '2Gi') +
    if $._config.stateful_queriers then
      container.withVolumeMountsMixin([
        volumeMount.new('querier-data', '/data'),
      ]) else {},

  local deployment = k.apps.v1.deployment,

  querier_deployment: if !$._config.stateful_queriers then
    deployment.new('querier', 3, [$.querier_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity
  else {},

  // PVC for queriers when running as statefulsets
  querier_data_pvc:: if $._config.stateful_queriers then
    pvc.new('querier-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.querier_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.querier_pvc_class)
  else {},

  querier_statefulset: if $._config.stateful_queriers then
    statefulSet.new('querier', 3, [$.querier_container], $.querier_data_pvc) +
    statefulSet.mixin.spec.withServiceName('querier') +
    statefulSet.mixin.spec.withPodManagementPolicy('Parallel') +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001)  // 10001 is the group ID assigned to Loki in the Dockerfile
  else {},

  querier_service:
    if !$._config.stateful_queriers then
      k.util.serviceFor($.querier_deployment)
    else
      k.util.serviceFor($.querier_statefulset),
}
