local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,
  local pvc = k.core.v1.persistentVolumeClaim,
  local statefulSet = k.apps.v1.statefulSet,
  local volumeMount = k.core.v1.volumeMount,
  local service = k.core.v1.service,

  _config+:: {
    write_replicas: 3,
  },

  // The writers should persist index files on a persistent
  // volume in order to be crash resilient.
  write_pvc::
    pvc.new('write-data') +
    pvc.mixin.spec.resources.withRequests({ storage: '10Gi' }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName('fast'),

  write_args::
    $._config.commonArgs {
      target: 'write',
    },

  write_container::
    container.new('write', $._images.write) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.write_args)) +
    container.withVolumeMountsMixin([volumeMount.new('write-data', '/data')]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1),

  write_statefulset:
    statefulSet.new('write', $._config.write_replicas, [$.write_container], $.write_pvc) +
    statefulSet.mixin.spec.withServiceName('write') +
    statefulSet.mixin.metadata.withLabels({ app: $._config.headless_service_name, name: 'write' }) +
    statefulSet.mixin.spec.selector.withMatchLabels({ name: 'write' }) +
    statefulSet.mixin.spec.template.metadata.withLabels({ name: 'write', app: $._config.headless_service_name }) +
    $._config.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki') +
    k.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    statefulSet.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800) +
    statefulSet.mixin.spec.withPodManagementPolicy('Parallel'),

  write_service:
    k.util.serviceFor($.write_statefulset) +
    service.mixin.spec.withType('ClusterIP') +
    service.mixin.spec.withPorts([
      k.core.v1.servicePort.newNamed('write-http-metrics', 80, 'http-metrics'),
      k.core.v1.servicePort.newNamed('write-grpc', 9095, 'grpc'),
    ]),
}
