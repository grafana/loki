{
  local container = $.core.v1.container,
  local pvc = $.core.v1.persistentVolumeClaim,
  local volumeMount = $.core.v1.volumeMount,
  local statefulSet = $.apps.v1.statefulSet,

  ingester_args::
    $._config.commonArgs {
      target: 'ingester',
    } + if $._config.stateful_ingesters then
    {
      // Disable chunk transfer when using statefulset since ingester which is going down won't find another
      // ingester which is joining the ring for transferring chunks.
      'ingester.max-transfer-retries': 0,
    } else {},

  ingester_container::
    container.new('ingester', $._images.ingester) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.ingester_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    $.util.resourcesRequests($._config.ingester.cpuRequests, $._config.ingester.memoryRequests) +
    $.util.resourcesLimits($._config.ingester.cpuLimits, $._config.ingester.memoryLimits) +
    if $._config.stateful_ingesters then
        container.withVolumeMountsMixin([
          volumeMount.new('ingester-data', '/data'),
        ]) else {},

  local deployment = $.apps.v1.deployment,

  local name = 'ingester',

  ingester_deployment: if !$._config.stateful_ingesters then
    deployment.new(name, $._config.ingester.replicas, [$.ingester_container]) +
    deployment.spec.template.spec.withTolerations($._config.tolerations) +
    $.config_hash_mixin +
    deployment.mixin.spec.template.metadata.withLabelsMixin($._config.labels) +
    deployment.mixin.spec.template.metadata.withAnnotationsMixin($._config.annotations) +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity +
    deployment.mixin.spec.withMinReadySeconds(60) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(0) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1) +
    deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800)
    else {},

  ingester_data_pvc:: if $._config.stateful_ingesters then
    pvc.new('ingester-data') +
    pvc.mixin.spec.resources.withRequests({ storage: '10Gi' }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName('fast')
    else {},

  ingester_statefulset: if $._config.stateful_ingesters then
    statefulSet.new('ingester', $._config.ingester.replicas, [$.ingester_container], $.ingester_data_pvc) +
    statefulSet.mixin.spec.withServiceName('ingester') +
    statefulSet.spec.template.spec.withTolerations($._config.tolerations) +
    $.config_hash_mixin +
    statefulSet.mixin.spec.template.metadata.withLabelsMixin($._config.labels) +
    statefulSet.mixin.spec.template.metadata.withAnnotationsMixin($._config.annotations) +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount('overrides', '/etc/loki/overrides') +
    $.util.antiAffinity +
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    statefulSet.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800)
    else {},

  ingester_service:
    if !$._config.stateful_ingesters then
      $.util.serviceFor($.ingester_deployment)
    else
      $.util.serviceFor($.ingester_statefulset),

  local podDisruptionBudget = $.policy.v1beta1.podDisruptionBudget,

  ingester_pdb:
    podDisruptionBudget.new() +
    podDisruptionBudget.mixin.metadata.withName('loki-ingester-pdb') +
    podDisruptionBudget.mixin.metadata.withLabels({ name: 'loki-ingester-pdb' }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ name: name }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(1),
}
