{
  local container = $.core.v1.container,
  local containerPort = $.core.v1.containerPort,

  distributor_args::
    $._config.commonArgs {
      target: 'distributor',
    },

  distributor_container::
    container.new('distributor', $._images.distributor) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.distributor_args)),

  local deployment = $.apps.v1beta1.deployment,

  distributor_deployment:
    deployment.new('distributor', 3, [$.distributor_container]) +
    $.config_hash_mixin +
    $.util.configVolumeMount('loki', '/etc/loki') +
    $.util.antiAffinity,

  distributor_service:
    $.util.serviceFor($.distributor_deployment),
}
