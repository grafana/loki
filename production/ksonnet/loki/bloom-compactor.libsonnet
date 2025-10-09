{
  local k = import 'ksonnet-util/kausal.libsonnet',
  local container = k.core.v1.container,
  local containerPort = k.core.v1.containerPort,
  local pvc = k.core.v1.persistentVolumeClaim,
  local service = k.core.v1.service,
  local statefulSet = k.apps.v1.statefulSet,
  local volume = k.core.v1.volume,
  local volumeMount = k.core.v1.volumeMount,

  _config+:: {
    bloom_compactor+: if !$._config.use_bloom_filters then {} else {
      // TODO(salvacorts): Configure autoscaling
      replicas: error 'missing replicas',
    },

  },

  local name = 'bloom-compactor',
  local volumeName = name + '-data',
  local volumeMounts = [volumeMount.new(volumeName, '/data')],

  // TODO(owen-d): removed PVCs when we can run the bloom-shipper in memory only
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

  bloom_compactor_args:: $._config.commonArgs {
    target: 'bloom-compactor',
  },

  bloom_compactor_ports:: [
    containerPort.new(name='http-metrics', port=$._config.http_listen_port),
    containerPort.new(name='grpc', port=9095),
  ],

  bloom_compactor_container:: if !$._config.use_bloom_filters then {} else
    container.new(name, $._images.bloom_compactor)
    // add default ports
    + container.withPorts($.bloom_compactor_ports)
    // add target specific CLI arguments
    + container.withArgsMixin(k.util.mapToFlags($.bloom_compactor_args))
    // add global environment variables
    + container.withEnvMixin($._config.commonEnvs)
    // mount the data pvc at given mountpoint
    + container.withVolumeMountsMixin(volumeMounts)
    // add HTTP readiness probe
    + container.mixin.readinessProbe.httpGet.withPath('/ready')
    + container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port)
    + container.mixin.readinessProbe.withTimeoutSeconds(1)
    // TODO(salvacorts): Estimate the right values for resources
    // define container resource requests
    + k.util.resourcesRequests('1', '4Gi')
    // define container resource limits
    + k.util.resourcesLimits(null, '8Gi'),

  bloom_compactor_statefulset: if !$._config.use_bloom_filters then {} else
    statefulSet.new(name, $._config.bloom_compactor.replicas, [$.bloom_compactor_container])
    + statefulSet.spec.withVolumeClaimTemplatesMixin($.bloom_compactor_data_pvc)
    // + statefulSet.mixin.spec.withVolumeClaimTemplatesMixin(volumeClaimTemplates)
    // add clusterIP service
    + statefulSet.mixin.spec.withServiceName(name)
    // perform rolling update when statefulset configuration changes
    + statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate')
    // TODO(owen-d): enable this once supported (currently alpha)
    // https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#rolling-updates
    // allow 50% of pods to be unavailable during upgrades
    // + statefulSet.mixin.spec.updateStrategy.rollingUpdate.withMaxUnavailable('50%') +

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
    + k.util.configVolumeMount('overrides', '/etc/loki/overrides'),

  bloom_compactor_service: if !$._config.use_bloom_filters then {} else
    k.util.serviceFor($.bloom_compactor_statefulset, $._config.service_ignored_labels),

  bloom_compactor_headless_service: if !$._config.use_bloom_filters then {} else
    k.util.serviceFor($.bloom_compactor_statefulset, $._config.service_ignored_labels)
    + service.mixin.metadata.withName(name + '-headless')
    + service.mixin.spec.withClusterIp('None'),
}
