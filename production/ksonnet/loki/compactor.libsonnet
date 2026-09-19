{
  local k = import 'ksonnet-util/kausal.libsonnet',
  local pvc = k.core.v1.persistentVolumeClaim,
  local volumeMount = k.core.v1.volumeMount,
  local container = k.core.v1.container,
  local statefulSet = k.apps.v1.statefulSet,
  local service = k.core.v1.service,
  local containerPort = k.core.v1.containerPort,
  local deployment = k.apps.v1.deployment,

  _config+:: {
    compactor_pvc_size: '10Gi',
    compactor_pvc_class: 'fast',

    enable_horizontally_scalable_compactor: false,

    loki+: {
      compactor+: {
        working_directory: '/data/compactor',
      },
    },
  },

  // Use PVC for compactor instead of node disk.
  compactor_data_pvc::
    pvc.new('compactor-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.compactor_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.compactor_pvc_class)
  ,

  compactor_args:: $._config.commonArgs {
    target: 'compactor',
  } + if $._config.enable_horizontally_scalable_compactor then {
    'compactor.horizontal-scaling-mode': 'main',
  } else {},

  compactor_ports:: $.util.defaultPorts,

  compactor_container::
    container.new('compactor', $._images.compactor) +
    container.withPorts($.compactor_ports) +
    container.withArgsMixin(k.util.mapToFlags($.compactor_args)) +
    container.withVolumeMountsMixin([volumeMount.new('compactor-data', '/data')]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('4', '2Gi') +
    k.util.resourcesLimits(null, '4Gi') +
    container.withEnvMixin($._config.commonEnvs)
  ,

  compactor_statefulset:
    statefulSet.new('compactor', 1, [$.compactor_container], $.compactor_data_pvc) +
    statefulSet.mixin.spec.withServiceName('compactor') +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001)  // 10001 is the group ID assigned to Loki in the Dockerfile
  ,

  compactor_service:
    k.util.serviceFor($.compactor_statefulset, $._config.service_ignored_labels)
  ,

  compactor_worker_args:: if $._config.enable_horizontally_scalable_compactor then $._config.commonArgs {
    target: 'compactor',
    'compactor.horizontal-scaling-mode': 'worker',
  },

  compactor_worker_container:: if !$._config.enable_horizontally_scalable_compactor then {} else
    container.new('compactor-worker', $._images.compactor_worker) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.compactor_worker_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    container.withEnvMixin($._config.commonEnvs) +
    $.jaeger_mixin +
    k.util.resourcesRequests('1', '500Mi') +
    k.util.resourcesLimits('2', '1Gi') +
    container.withEnvMixin($._config.commonEnvs),

  compactor_worker_deployment: if !$._config.enable_horizontally_scalable_compactor then {} else
    deployment.new('compactor-worker', 1, [$.compactor_worker_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge('15%') +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable('15%'),
}
