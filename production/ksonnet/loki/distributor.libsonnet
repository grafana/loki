local k = import 'ksonnet-util/kausal.libsonnet';
{
  local container = k.core.v1.container,
  local containerPort = k.core.v1.containerPort,

  distributor_args::
    $._config.commonArgs {
      target: 'distributor',
    },

  distributor_ports:: $.util.defaultPorts,

  distributor_container::
    container.new('distributor', $._images.distributor) +
    container.withPorts($.distributor_ports) +
    container.withArgsMixin(k.util.mapToFlags($.distributor_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('500m', '500Mi') +
    k.util.resourcesLimits('1', '1Gi') +
    container.withEnvMixin($._config.commonEnvs),

  local deployment = k.apps.v1.deployment,

  distributor_deployment:
    deployment.new('distributor', 3, [$.distributor_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(5) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1),

  distributor_service:
    k.util.serviceFor($.distributor_deployment, $._config.service_ignored_labels),
}
