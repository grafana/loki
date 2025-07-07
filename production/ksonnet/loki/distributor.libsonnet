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
    k.util.resourcesRequests('500m', '2500Mi') +
    k.util.resourcesLimits(null, '5Gi') +
    container.withEnvMixin($._config.commonEnvs),

  local deployment = k.apps.v1.deployment,
  local topologySpreadConstraints = k.core.v1.topologySpreadConstraint,

  distributor_deployment:
    deployment.new('distributor', 3, [$.distributor_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(5) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1) +
    if $._config.distributor.no_schedule_constraints then {}
    else if $._config.distributor.use_topology_spread then
      deployment.spec.template.spec.withTopologySpreadConstraints(
        // Evenly spread queriers among available nodes.
        topologySpreadConstraints.labelSelector.withMatchLabels({ name: 'distributor' }) +
        topologySpreadConstraints.withTopologyKey('kubernetes.io/hostname') +
        topologySpreadConstraints.withWhenUnsatisfiable('ScheduleAnyway') +
        topologySpreadConstraints.withMaxSkew($._config.distributor.topology_spread_max_skew),
      )
    else
      k.util.antiAffinity,

  distributor_pdb:
    local podDisruptionBudget = k.policy.v1.podDisruptionBudget;

    podDisruptionBudget.new('distributor-pdb') +
    podDisruptionBudget.mixin.metadata.withLabels({ name: 'distributor-pdb' }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ name: 'distributor' }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(1),

  distributor_service:
    k.util.serviceFor($.distributor_deployment, $._config.service_ignored_labels),
}
