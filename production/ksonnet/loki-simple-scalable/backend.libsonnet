local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,
  local pvc = k.core.v1.persistentVolumeClaim,
  local statefulSet = k.apps.v1.statefulSet,
  local volumeMount = k.core.v1.volumeMount,
  local service = k.core.v1.service,

  _config+:: {
    backend_replicas: 1,
  },

  // The backends
  backend_pvc::
    pvc.new('backend-data') +
    pvc.mixin.spec.resources.withRequests({ storage: '10Gi' }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName('fast'),

  backend_args::
    $._config.commonArgs {
      target: 'backend',
    },

  backend_container::
    container.new('backend', $._images.loki) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.backend_args)) +
    container.withVolumeMountsMixin([volumeMount.new('backend-data', '/data')]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1),

  backend_statefulset:
    statefulSet.new('backend', $._config.backend_replicas, [$.backend_container], $.backend_pvc) +
    statefulSet.mixin.spec.withServiceName('backend') +
    statefulSet.mixin.metadata.withLabels({ app: $._config.headless_service_name, name: 'backend' }) +
    statefulSet.mixin.spec.selector.withMatchLabels({ name: 'backend' }) +
    statefulSet.mixin.spec.template.metadata.withLabels({ name: 'backend', app: $._config.headless_service_name }) +
    $._config.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki') +
    k.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    statefulSet.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800) +
    statefulSet.mixin.spec.withPodManagementPolicy('Parallel'),

  backend_service:
    k.util.serviceFor($.backend_statefulset) +
    service.mixin.spec.withType('ClusterIP') +
    service.mixin.spec.withPorts([
      k.core.v1.servicePort.newNamed('backend-http-metrics', 80, 'http-metrics'),
      k.core.v1.servicePort.newNamed('backend-grpc', 9095, 'grpc'),
    ]),
}
