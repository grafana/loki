{
  local container = $.core.v1.container,

  querier_args::
    $._config.ringConfig {
      target: 'querier',
      'distributor.replication-factor': $._config.replication_factor,
    },

  querier_container::
    container.new('querier', $._images.tempo) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.querier_args)),

  local deployment = $.apps.v1beta1.deployment,

  querier_deployment:
    deployment.new('querier', 3, [$.querier_container]) +
    $.util.antiAffinity,

  querier_service:
    $.util.serviceFor($.querier_deployment),
}
