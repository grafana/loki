local k = import 'ksonnet-util/kausal.libsonnet';
{
  local container = k.core.v1.container,
  local pvc = k.core.v1.persistentVolumeClaim,
  local volumeMount = k.core.v1.volumeMount,
  local statefulSet = k.apps.v1.statefulSet,

  ingester_args::
    $._config.commonArgs {
      target: 'ingester',
    } + if $._config.stateful_ingesters then
      {
        // Disable chunk transfer when using statefulset since ingester which is going down won't find another
        // ingester which is joining the ring for transferring chunks.
        'ingester.max-transfer-retries': 0,
      } else {},

  ingester_ports: $.util.defaultPorts,

  ingester_container::
    container.new('ingester', $._images.ingester) +
    container.withPorts($.ingester_ports) +
    container.withArgsMixin(k.util.mapToFlags($.ingester_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('1', '5Gi') +
    k.util.resourcesLimits('2', '10Gi') +
    container.withEnvMixin($._config.commonEnvs) +
    if $._config.stateful_ingesters then
      container.withVolumeMountsMixin([
        volumeMount.new('ingester-data', '/data'),
      ]) else {},

  local deployment = k.apps.v1.deployment,

  local name = 'ingester',

  ingester_deployment: if !$._config.stateful_ingesters then
    deployment.new(name, 3, [$.ingester_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity +
    deployment.mixin.spec.withMinReadySeconds(60) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(0) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1) +
    deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800)
  else {},

  ingester_data_pvc:: if $._config.stateful_ingesters then
    pvc.new('ingester-data') +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.ingester_pvc_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.ingester_pvc_class)
  else {},

  ingester_statefulset: if $._config.stateful_ingesters then
    statefulSet.new('ingester', 3, [$.ingester_container], $.ingester_data_pvc) +
    statefulSet.mixin.spec.withServiceName('ingester') +
    statefulSet.mixin.spec.withPodManagementPolicy('Parallel') +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    statefulSet.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800)
  else {},

  ingester_service:
    if !$._config.stateful_ingesters then
      k.util.serviceFor($.ingester_deployment, $._config.service_ignored_labels)
    else
      k.util.serviceFor($.ingester_statefulset, $._config.service_ignored_labels),

  local podDisruptionBudget = k.policy.v1beta1.podDisruptionBudget,

  ingester_pdb:
    podDisruptionBudget.new() +
    podDisruptionBudget.mixin.metadata.withName('loki-ingester-pdb') +
    podDisruptionBudget.mixin.metadata.withLabels({ name: 'loki-ingester-pdb' }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ name: name }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(1),
}
