local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,
  local deployment = k.apps.v1.deployment,
  local volumeMount = k.core.v1.volumeMount,
  local service = k.core.v1.service,

  _config+:: {
    read_replicas: 3,
  },

  read_args::
    $._config.commonArgs {
      target: 'read',
    },

  read_container::
    container.new('read', $._images.read) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.read_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1),

  read_deployment:
    deployment.new('read', $._config.read_replicas, [$.read_container]) +
    deployment.mixin.metadata.withLabels({ app: $._config.headless_service_name, name: 'read' }) +
    deployment.mixin.spec.selector.withMatchLabels({ name: 'read' }) +
    deployment.mixin.spec.template.metadata.withLabels({ name: 'read', app: $._config.headless_service_name }) +
    $._config.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki') +
    k.util.antiAffinity +
    deployment.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800),

  read_service:
    k.util.serviceFor($.read_deployment) +
    service.mixin.spec.withType('ClusterIP') +
    service.mixin.spec.withPorts([
      k.core.v1.servicePort.newNamed('read-http-metrics', 80, 'http-metrics'),
      k.core.v1.servicePort.newNamed('read-grpc', 9095, 'grpc'),
    ]),
}
