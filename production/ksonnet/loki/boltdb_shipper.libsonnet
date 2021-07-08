{
  local k = import 'ksonnet-util/kausal.libsonnet',
  local pvc = k.core.v1.persistentVolumeClaim,
  local volumeMount = k.core.v1.volumeMount,
  local container = k.core.v1.container,
  local statefulSet = k.apps.v1.statefulSet,
  local service = k.core.v1.service,
  local containerPort = k.core.v1.containerPort,

  _config+:: {
    // run ingesters and queriers as statefulsets when using boltdb-shipper to avoid using node disk for storing the index.
    stateful_ingesters: if self.using_boltdb_shipper then true else super.stateful_ingesters,
    stateful_queriers: if self.using_boltdb_shipper && !self.use_index_gateway then true else super.stateful_queriers,

    boltdb_shipper_shared_store: error 'must define boltdb_shipper_shared_store when using_boltdb_shipper=true. If this is not intentional, consider disabling it. boltdb_shipper_shared_store is a backend key from the storage_config, such as (gcs) or (s3)',
    compactor_pvc_size: '10Gi',
    compactor_pvc_class: 'fast',
    index_period_hours: if self.using_boltdb_shipper then 24 else super.index_period_hours,
    loki+: if self.using_boltdb_shipper then {
      chunk_store_config+: {
        write_dedupe_cache_config:: {},
      },
      storage_config+: {
        boltdb_shipper+: {
          shared_store: $._config.boltdb_shipper_shared_store,
          active_index_directory: '/data/index',
          cache_location: '/data/boltdb-cache',
        },
      },
    } else {},
  },

  // we don't dedupe index writes when using boltdb-shipper so don't deploy a cache for it.
  memcached_index_writes: if $._config.using_boltdb_shipper then {} else super.memcached_index_writes,

  // Use PVC for compactor instead of node disk.
  compactor_data_pvc:: if $._config.using_boltdb_shipper then
    pvc.new('compactor-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.compactor_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.compactor_pvc_class)
  else {},

  compactor_args:: if $._config.using_boltdb_shipper then {
    'config.file': '/etc/loki/config/config.yaml',
    'boltdb.shipper.compactor.working-directory': '/data/compactor',
    'boltdb.shipper.compactor.shared-store': $._config.boltdb_shipper_shared_store,
    target: 'compactor',
  } else {},

  local compactor_ports =
    [
      containerPort.new(name='http-metrics', port=$._config.http_listen_port),
    ],

  compactor_container:: if $._config.using_boltdb_shipper then
    container.new('compactor', $._images.compactor) +
    container.withPorts(compactor_ports) +
    container.withArgsMixin(k.util.mapToFlags($.compactor_args)) +
    container.withVolumeMountsMixin([volumeMount.new('compactor-data', '/data')]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('4', '2Gi')
  else {},

  compactor_statefulset: if $._config.using_boltdb_shipper then
    statefulSet.new('compactor', 1, [$.compactor_container], $.compactor_data_pvc) +
    statefulSet.mixin.spec.withServiceName('compactor') +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001)  // 10001 is the group ID assigned to Loki in the Dockerfile
  else {},
}
