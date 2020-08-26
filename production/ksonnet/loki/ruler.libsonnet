{
  local container = $.core.v1.container,

  ruler_args:: $._config.commonArgs {
    target: 'ruler',
  },

  ruler_container::
    if $._config.ruler_enabled then
      container.new('ruler', $._images.ruler) +
      container.withPorts($.util.defaultPorts) +
      container.withArgsMixin($.util.mapToFlags($.ruler_args)) +
      $.util.resourcesRequests('1', '6Gi') +
      $.util.resourcesLimits('16', '16Gi') +
      $.util.readinessProbe +
      $.jaeger_mixin
    else {},

  local deployment = $.apps.v1.deployment,

  ruler_deployment:
    if $._config.ruler_enabled then
      deployment.new('ruler', 2, [$.ruler_container]) +
      deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(600) +
      $.config_hash_mixin +
      $.util.configVolumeMount('loki', '/etc/loki/config') +
      $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
      $.util.antiAffinity
    else {},

  local service = $.core.v1.service,

  ruler_service:
    if $._config.ruler_enabled then
      $.util.serviceFor($.ruler_deployment)
    else {},
}
