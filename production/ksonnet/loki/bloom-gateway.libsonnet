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
    bloom_gateway+: if !$._config.use_bloom_filters then {} else {
      // TODO(salvacorts): Configure autoscaling
      replicas: error 'missing replicas',

      ////** Storage **////
      // if true, the host needs to have local SSD disks mounted, otherwise PVCs are used
      use_local_ssd: false,
      // PVC config
      pvc_size: if !self.use_local_ssd then error 'bloom_gateway.pvc_size needs to be defined when using PVC' else '',
      pvc_class: if !self.use_local_ssd then error 'bloom_gateway.pvc_class needs to be defined when using PVC' else '',
      // local SSD config
      hostpaths: if self.use_local_ssd then error 'bloom_gateway.hostpaths needs to be defined when using local SSDs' else [],
      node_selector: if self.use_local_ssd then error 'bloom_gateway.node_selector needs to be defined when using local SSDs' else {},
      tolerations: if self.use_local_ssd then error 'bloom_gateway.tolerations needs to be defined when using local SSDs' else [],
    },
  },

  local name = 'bloom-gateway',

  local paths = std.range(0, std.length($._config.bloom_gateway.hostpaths) - 1),

  local volumeNames = [
    '%s-data-%d' % [name, x]
    for x in paths
  ],

  local volumes =
    if $._config.bloom_gateway.use_local_ssd
    then [
      volume.fromHostPath(volumeNames[x], $._config.bloom_gateway.hostpaths[x])
      for x in paths
    ]
    else [],

  local volumeMounts = [
    volumeMount.new(volumeNames[x], '/data%d' % [x])
    for x in paths
  ],

  bloom_gateway_args:: $._config.commonArgs {
    target: 'bloom-gateway',
  },

  bloom_gateway_ports:: [
    containerPort.new(name='http-metrics', port=$._config.http_listen_port),
    containerPort.new(name='grpc', port=9095),
  ],

  bloom_gateway_data_pvc:: if !$._config.use_bloom_filters || !$._config.bloom_gateway.use_local_ssd then null else
    pvc.new('%s-data' % name)
    // set disk size
    + pvc.mixin.spec.resources.withRequests({ storage: $._config.bloom_gateway.pvc_size })
    // mount the volume as read-write by a single node
    + pvc.mixin.spec.withAccessModes(['ReadWriteOnce'])
    // set persistent volume storage class
    + pvc.mixin.spec.withStorageClassName($._config.bloom_gateway.pvc_class),

  bloom_gateway_container:: if !$._config.use_bloom_filters then {} else
    container.new(name, $._images.bloom_gateway)
    // add default ports
    + container.withPorts($.bloom_gateway_ports)
    // add target specific CLI arguments
    + container.withArgsMixin(k.util.mapToFlags($.bloom_gateway_args))
    // mount local SSD or PVC
    + container.withVolumeMountsMixin(volumeMounts)
    // add globale environment variables
    + container.withEnvMixin($._config.commonEnvs)
    // add HTTP readiness probe
    + container.mixin.readinessProbe.httpGet.withPath('/ready')
    + container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port)
    + container.mixin.readinessProbe.withTimeoutSeconds(1)
    // define container resource requests
    + k.util.resourcesRequests('4', '4Gi')
    // define container resource limits
    + k.util.resourcesLimits(null, '8Gi'),

  bloom_gateway_statefulset: if !$._config.use_bloom_filters then {} else
    statefulSet.new(name, $._config.bloom_gateway.replicas, [$.bloom_gateway_container])
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
    // configuration specific to SSD/PVC usage
    + (
      if $._config.bloom_gateway.use_local_ssd
      then
        // ensure the pod is scheduled on a node with local SSDs if needed
        statefulSet.mixin.spec.template.spec.withNodeSelector($._config.bloom_gateway.node_selector)
        // tolerate the local-ssd taint
        + statefulSet.mixin.spec.template.spec.withTolerationsMixin($._config.bloom_gateway.tolerations)
        // mount the local SSDs
        + statefulSet.mixin.spec.template.spec.withVolumesMixin(volumes)
      else
        // create persistent volume claim
        statefulSet.mixin.spec.withVolumeClaimTemplates([$.bloom_gateway_data_pvc])
    ),

  bloom_gateway_service: if !$._config.use_bloom_filters then {} else
    k.util.serviceFor($.bloom_gateway_statefulset, $._config.service_ignored_labels),

  bloom_gateway_headless_service: if !$._config.use_bloom_filters then {} else
    k.util.serviceFor($.bloom_gateway_statefulset, $._config.service_ignored_labels)
    + service.mixin.metadata.withName(name + '-headless')
    + service.mixin.spec.withClusterIp('None'),
}
