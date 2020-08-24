{
  local pvc = $.core.v1.persistentVolumeClaim,
  local volumeMount = $.core.v1.volumeMount,
  local container = $.core.v1.container,
  local statefulSet = $.apps.v1.statefulSet,
  local service = $.core.v1.service,
  local containerPort = $.core.v1.containerPort,

  _config+:: {
    // configurable pvc sizes
    ingester_pvc_size: '5Gi',
    querier_pvc_size: '10Gi',
    compactor_pvc_size: '10Gi',

    boltdb_shipper_shared_store: error 'must define boltdb_shipper_shared_store',
    index_period_hours: 24,
    loki+: {
      chunk_store_config+: {
        write_dedupe_cache_config:: {},
      },
      storage_config+: {
        boltdb_shipper: {
          shared_store: $._config.boltdb_shipper_shared_store,
        },
      },
    },
  },

  // The ingesters should persist index files on a persistent
  // volume in order to be crash resilient.
  local ingester_data_pvc =
    pvc.new('ingester-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.ingester_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName('fast'),

  ingester_deployment: {},

  ingester_args+:: {
    // Persist index in pvc
    'boltdb.shipper.active-index-directory': '/data/index',

    // Use PVC for caching
    'boltdb.shipper.cache-location': '/data/boltdb-cache',

    // Disable chunk transfer
    'ingester.max-transfer-retries': 0,
  },

  ingester_container+::
    container.withVolumeMountsMixin([
      volumeMount.new('ingester-data', '/data'),
    ]),

  ingester_statefulset:
    statefulSet.new('ingester', 3, [$.ingester_container], ingester_data_pvc) +
    statefulSet.mixin.spec.withServiceName('ingester') +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    statefulSet.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800),

  ingester_service:
    $.util.serviceFor($.ingester_statefulset),

  // Use PVC for queriers instead of node disk.
  local querier_data_pvc =
    pvc.new('querier-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.querier_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName('fast'),

  querier_deployment: {},

  querier_args+:: {
    // Use PVC for caching
    'boltdb.shipper.cache-location': '/data/boltdb-cache',
  },

  querier_container+::
    container.withVolumeMountsMixin([
      volumeMount.new('querier-data', '/data'),
    ]),

  querier_statefulset:
    statefulSet.new('querier', 3, [$.querier_container], querier_data_pvc) +
    statefulSet.mixin.spec.withServiceName('querier') +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001),  // 10001 is the group ID assigned to Loki in the Dockerfile

  querier_service:
    $.util.serviceFor($.querier_statefulset),

  memcached_index_writes:: {}, // we don't dedupe index writes when using boltdb-shipper so don't deploy a cache for it.

  // Use PVC for compactor instead of node disk.
  local compactor_data_pvc =
    pvc.new('compactor-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.compactor_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName('fast'),

  compactor_args::
    {
      'config.file': '/etc/loki/config/config.yaml',
      'boltdb.shipper.compactor.working-directory': '/data/compactor',
      'boltdb.shipper.compactor.shared-store': $._config.boltdb_shipper_shared_store,
      target: 'compactor',
    },

  local compactor_ports =
    [
      containerPort.new(name='http-metrics', port=$._config.http_listen_port),
    ],

  compactor_container::
    container.new('compactor', $._images.compactor) +
    container.withPorts(compactor_ports) +
    container.withArgsMixin($.util.mapToFlags($.compactor_args)) +
    container.withVolumeMountsMixin([volumeMount.new('compactor-data', '/data')]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    $.util.resourcesRequests('4', '2Gi'),

  compactor_statefulset:
    statefulSet.new('compactor', 1, [$.compactor_container], compactor_data_pvc) +
    statefulSet.mixin.spec.withServiceName('compactor') +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001),  // 10001 is the group ID assigned to Loki in the Dockerfile

  compactor_service:
    $.util.serviceFor($.compactor_statefulset),
}
