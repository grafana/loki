local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,
  local pvc = k.core.v1.persistentVolumeClaim,
  local statefulSet = k.apps.v1.statefulSet,
  local deployment = k.apps.v1.deployment,
  local volumeMount = k.core.v1.volumeMount,

  ruler_args:: $._config.commonArgs {
    target: 'ruler',
  } + if $._config.using_boltdb_shipper && !$._config.using_tsdb_shipper then {
    // Use PVC for caching
    'boltdb.shipper.cache-location': '/data/boltdb-cache',
  } else {},

  _config+:: {
    // run rulers as statefulsets when using boltdb-shipper to avoid using node disk for storing the index.
    stateful_rulers: if self.using_boltdb_shipper && !self.use_index_gateway then true else super.stateful_rulers,
  },

  ruler_ports:: $.util.defaultPorts,

  ruler_container::
    if $._config.ruler_enabled then
      container.new('ruler', $._images.ruler) +
      container.withPorts($.ruler_ports) +
      container.withArgsMixin(k.util.mapToFlags($.ruler_args)) +
      k.util.resourcesRequests('1', '6Gi') +
      k.util.resourcesLimits('16', '16Gi') +
      $.util.readinessProbe +
      container.withEnvMixin($._config.commonEnvs) +
      $.jaeger_mixin +
      if $._config.stateful_rulers then
        container.withVolumeMountsMixin([
          volumeMount.new('ruler-data', '/data'),
        ]) else {}
    else {},

  ruler_deployment:
    if $._config.ruler_enabled && !$._config.stateful_rulers then
      deployment.new('ruler', 2, [$.ruler_container]) +
      deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(600) +
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

  ruler_service: if !$._config.ruler_enabled
  then {}
  else
    if $._config.stateful_rulers
    then k.util.serviceFor($.ruler_statefulset, $._config.service_ignored_labels)
    else k.util.serviceFor($.ruler_deployment, $._config.service_ignored_labels),


  // PVC for rulers when running as statefulsets
  ruler_data_pvc:: if $._config.ruler_enabled && $._config.stateful_rulers then
    pvc.new('ruler-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.ruler_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.ruler_pvc_class)
  else {},

  ruler_statefulset: if $._config.ruler_enabled && $._config.stateful_rulers then
    statefulSet.new('ruler', 2, [$.ruler_container], $.ruler_data_pvc) +
    statefulSet.mixin.spec.withServiceName('ruler') +
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
}
