{
  local k = import 'ksonnet-util/kausal.libsonnet',
  local pvc = k.core.v1.persistentVolumeClaim,
  local volumeMount = k.core.v1.volumeMount,
  local container = k.core.v1.container,
  local statefulSet = k.apps.v1.statefulSet,
  local service = k.core.v1.service,
  local containerPort = k.core.v1.containerPort,

  _config+:: {
    // flag for tuning things when boltdb-shipper is current or upcoming index type.
    using_boltdb_shipper: true,
    using_tsdb_shipper: false,
    using_shipper_store: $._config.using_boltdb_shipper || $._config.using_tsdb_shipper,

    stateful_queriers: if self.using_shipper_store && !self.use_index_gateway then true else super.stateful_queriers,

    compactor_pvc_size: '10Gi',
    compactor_pvc_class: 'fast',
    index_period_hours: if self.using_shipper_store then 24 else super.index_period_hours,
    loki+: if self.using_shipper_store then {
      storage_config+: {
        boltdb_shipper+: if $._config.using_boltdb_shipper then {
          active_index_directory: '/data/index',
          cache_location: '/data/boltdb-cache',
        } else {},
        tsdb_shipper+: if $._config.using_tsdb_shipper then {
          active_index_directory: '/data/tsdb-index',
          cache_location: '/data/tsdb-cache',
        } else {},
      },
      compactor+: {
        working_directory: '/data/compactor',
      },
    } else {},
  },

  // we don't dedupe index writes when using boltdb-shipper or tsdb-shipper so don't deploy a cache for it.
  memcached_index_writes: if $._config.using_shipper_store then {} else
    if 'memcached_index_writes' in super then super.memcached_index_writes else {},

  // Use PVC for compactor instead of node disk.
  compactor_data_pvc:: if $._config.using_shipper_store then
    pvc.new('compactor-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.compactor_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.compactor_pvc_class)
  else {},

  compactor_args:: if $._config.using_shipper_store then $._config.commonArgs {
    target: 'compactor',
  } else {},

  compactor_ports:: $.util.defaultPorts,

  compactor_container:: if $._config.using_shipper_store then
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
  else {},

  compactor_statefulset: if $._config.using_shipper_store then
    statefulSet.new('compactor', 1, [$.compactor_container], $.compactor_data_pvc) +
    statefulSet.mixin.spec.withServiceName('compactor') +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001)  // 10001 is the group ID assigned to Loki in the Dockerfile
  else {},

  compactor_service: if $._config.using_shipper_store then
    k.util.serviceFor($.compactor_statefulset, $._config.service_ignored_labels)
  else {},
}
