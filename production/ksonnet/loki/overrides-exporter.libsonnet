local k = import 'ksonnet-util/kausal.libsonnet';
{
  local container = k.core.v1.container,
  local containerPort = k.core.v1.containerPort,

  overrides_exporter_args::
    $._config.commonArgs {
      target: 'overrides-exporter',
    },

  overrides_exporter_container::
    container.new('overrides-exporter', $._images.overrides_exporter) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin(k.util.mapToFlags($.overrides_exporter_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('100m', '128Mi') +
    k.util.resourcesLimits('200', '512Mi'),

  local deployment = k.apps.v1.deployment,

  overrides_exporter_deployment: if $._config.overrides_exporter_enabled then
    deployment.new('overrides-exporter', 1, [$.overrides_exporter_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity
  else {},

  overrides_exporter_service: if $._config.overrides_exporter_enabled then
    k.util.serviceFor($.overrides_exporter_deployment, $._config.service_ignored_labels)
  else {},
}
