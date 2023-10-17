{
  _config+:: {
    use_index_gateway: false,
    index_gateway_pvc_size: '50Gi',
    index_gateway_pvc_class: 'fast',

    loki+: {
      storage_config+: if $._config.use_index_gateway then {
        boltdb_shipper+: {
          index_gateway_client+: {
            server_address: 'dns+index-gateway-headless.%s.svc.cluster.local:9095' % $._config.namespace,
          },
        },
        tsdb_shipper+: {
          index_gateway_client+: {
            server_address: 'dns+index-gateway-headless.%s.svc.cluster.local:9095' % $._config.namespace,
          },
        },
      } else {},
    },
  },

  local k = import 'ksonnet-util/kausal.libsonnet',
  local containerPort = k.core.v1.containerPort,
  local pvc = k.core.v1.persistentVolumeClaim,
  local container = k.core.v1.container,
  local volumeMount = k.core.v1.volumeMount,
  local statefulSet = k.apps.v1.statefulSet,

  // Use PVC for index_gateway instead of node disk.
  index_gateway_data_pvc:: if $._config.use_index_gateway then
    pvc.new('index-gateway-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.index_gateway_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.index_gateway_pvc_class)
  else {},

  index_gateway_args:: if $._config.use_index_gateway then $._config.commonArgs {
    target: 'index-gateway',
  } else {},

  index_gateway_ports: [
    containerPort.new(name='http-metrics', port=$._config.http_listen_port),
    containerPort.new(name='grpc', port=9095),
  ],

  index_gateway_container:: if $._config.use_index_gateway then
    container.new('index-gateway', $._images.index_gateway) +
    container.withPorts($.index_gateway_ports) +
    container.withArgsMixin(k.util.mapToFlags($.index_gateway_args)) +
    container.withVolumeMountsMixin([volumeMount.new('index-gateway-data', '/data')]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('500m', '2Gi') +
    container.withEnvMixin($._config.commonEnvs)
  else {},

  index_gateway_statefulset: if $._config.use_index_gateway then
    statefulSet.new('index-gateway', 1, [$.index_gateway_container], $.index_gateway_data_pvc) +
    statefulSet.mixin.spec.withServiceName('index-gateway') +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001)  // 10001 is the group ID assigned to Loki in the Dockerfile
  else {},

  index_gateway_service: if $._config.use_index_gateway then
    k.util.serviceFor($.index_gateway_statefulset, $._config.service_ignored_labels)
  else {},

  index_gateway_headless_service: if $._config.use_index_gateway then
    local service = k.core.v1.service;
    k.util.serviceFor($.index_gateway_statefulset, $._config.service_ignored_labels) +
    service.mixin.metadata.withName('index-gateway-headless') +
    service.mixin.spec.withClusterIp('None')
  else {},
}
