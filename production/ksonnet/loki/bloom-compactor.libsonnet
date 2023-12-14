{
  local k = import 'ksonnet-util/kausal.libsonnet',
  local container = k.core.v1.container,
  local containerPort = k.core.v1.containerPort,
  local pvc = k.core.v1.persistentVolumeClaim,
  local service = k.core.v1.service,
  local statefulSet = k.apps.v1.statefulSet,
  local volume = k.core.v1.volume,
  local volumeMount = k.core.v1.volumeMount,

  local name = 'bloom-compactor',

  _config+:: {
    bloom_compactor+: {
      // number of replicas
      replicas: if $._config.use_bloom_filters then 3 else 0,
      // PVC config
      pvc_size: if $._config.use_bloom_filters then error 'bloom_compactor.pvc_size needs to be defined' else '',
      pvc_class: if $._config.use_bloom_filters then error 'bloom_compactor.pvc_class needs to be defined' else '',
    },
    loki+:
      if $._config.use_bloom_filters
      then
        {
          bloom_compactor: {
            enabled: true,
            working_directory: '/data/blooms',
            compaction_interval: '15m',
            max_compaction_parallelism: 1,
          },
        }
      else {},
  },

  local cfg = self._config.bloom_compactor,

  local volumeName = name + '-data',
  local volumeMounts = [volumeMount.new(volumeName, '/data')],

  bloom_compactor_args::
    if $._config.use_bloom_filters
    then
      $._config.commonArgs {
        target: 'bloom-compactor',
      }
    else {},

  bloom_compactor_ports:: [
    containerPort.new(name='http-metrics', port=$._config.http_listen_port),
    containerPort.new(name='grpc', port=9095),
  ],

  bloom_compactor_data_pvc::
    if $._config.use_bloom_filters
    then
      pvc.new(volumeName)
      // set disk size
      + pvc.mixin.spec.resources.withRequests({ storage: $._config.bloom_compactor.pvc_size })
      // mount the volume as read-write by a single node
      + pvc.mixin.spec.withAccessModes(['ReadWriteOnce'])
      // set persistent volume storage class
      + pvc.mixin.spec.withStorageClassName($._config.bloom_compactor.pvc_class)
    else {},


  bloom_compactor_container::
    if $._config.use_bloom_filters
    then
      container.new(name, $._images.bloom_compactor)
      // add default ports
      + container.withPorts($.bloom_compactor_ports)
      // add target specific CLI arguments
      + container.withArgsMixin(k.util.mapToFlags($.bloom_compactor_args))
      // mount the data pvc at given mountpoint
      + container.withVolumeMountsMixin(volumeMounts)
      // add globale environment variables
      + container.withEnvMixin($._config.commonEnvs)
      // add HTTP readiness probe
      + container.mixin.readinessProbe.httpGet.withPath('/ready')
      + container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port)
      + container.mixin.readinessProbe.withTimeoutSeconds(1)
      // define container resource requests
      + k.util.resourcesRequests('2', '4Gi')
      // define container resource limits
      + k.util.resourcesLimits(null, '8Gi')
    else {},

  bloom_compactor_statefulset:
    if $._config.use_bloom_filters
    then
      statefulSet.new(name, cfg.replicas, [$.bloom_compactor_container], $.bloom_compactor_data_pvc)
      // add clusterIP service
      + statefulSet.mixin.spec.withServiceName(name)
      // perform rolling update when statefulset configuration changes
      + statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate')
      // launch or terminate pods in parallel, *does not* affect upgrades
      + statefulSet.mixin.spec.withPodManagementPolicy('Parallel')
      // 10001 is the user/group ID assigned to Loki in the Dockerfile
      + statefulSet.mixin.spec.template.spec.securityContext.withRunAsUser(10001)
      + statefulSet.mixin.spec.template.spec.securityContext.withRunAsGroup(10001)
      + statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001)
      // ensure statefulset is updated when loki config changes
      + $.config_hash_mixin
      // ensure no other workloads are scheduled
      + k.util.antiAffinity
      // mount the loki config.yaml
      + k.util.configVolumeMount('loki', '/etc/loki/config')
      // mount the runtime overrides.yaml
      + k.util.configVolumeMount('overrides', '/etc/loki/overrides')
    else {},

  bloom_compactor_service:
    if $._config.use_bloom_filters
    then
      k.util.serviceFor($.bloom_compactor_statefulset, $._config.service_ignored_labels)
    else {},

  bloom_compactor_headless_service:
    if $._config.use_bloom_filters
    then
      k.util.serviceFor($.bloom_compactor_statefulset, $._config.service_ignored_labels)
      + service.mixin.metadata.withName(name + '-headless')
      + service.mixin.spec.withClusterIp('None')
    else {},
}
