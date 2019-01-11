{
  local container = $.core.v1.container,

  querier_args::
    $._config.commonArgs {
      target: 'querier',
    },

  querier_container::
    container.new('querier', $._images.querier) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.querier_args)),

  local deployment = $.apps.v1beta1.deployment,

  querier_deployment:
    deployment.new('querier', 3, [$.querier_container]) +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki') +
    $.util.antiAffinity,

  querier_service:
    $.util.serviceFor($.querier_deployment),
}
