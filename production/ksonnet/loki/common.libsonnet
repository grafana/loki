local k = import 'ksonnet-util/kausal.libsonnet';

{
  namespace:
    k.core.v1.namespace.new($._config.namespace),

  jaeger_mixin+::
    if $._config.jaeger_agent_host == null
    then {}
    else
      k.core.v1.container.withEnvMixin([
        k.core.v1.container.envType.new('JAEGER_REPORTER_MAX_QUEUE_SIZE', std.toString($._config.jaeger_reporter_max_queue)),
      ]),

  util+:: {
    local containerPort = k.core.v1.containerPort,
    local container = k.core.v1.container,

    defaultPorts::
      [
        containerPort.new(name='http-metrics', port=$._config.http_listen_port),
        containerPort.new(name='grpc', port=9095),
      ],

    // We use DNS SRV record for discovering the schedulers or the frontends if schedulers are disabled.
    // These expect port names like _grpclb on the services.
    grpclbDefaultPorts:: [
      containerPort.new(name='http-metrics', port=$._config.http_listen_port),
      containerPort.new(name='grpclb', port=9095),
    ],

    // This helps ensure we create SRV records starting with _grpclb
    grpclbServiceFor(deployment):: k.util.serviceFor(deployment, $._config.service_ignored_labels, nameFormat='%(port)s'),

    // Headless service for discovering IPs of pods instead of the service IP.
    headlessService(deployment, name)::
      $.util.grpclbServiceFor(deployment) +
      k.core.v1.service.mixin.spec.withClusterIp('None') +
      k.core.v1.service.mixin.spec.withPublishNotReadyAddresses(true) +
      k.core.v1.service.mixin.metadata.withName(name),

    readinessProbe::
      container.mixin.readinessProbe.httpGet.withPath('/ready') +
      container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
      container.mixin.readinessProbe.withInitialDelaySeconds(15) +
      container.mixin.readinessProbe.withTimeoutSeconds(1),
  },

  // functions for k8s objects
  newLokiPdb(deploymentName, maxUnavailable=1)::
    local podDisruptionBudget = $.policy.v1.podDisruptionBudget;
    local pdbName = '%s-pdb' % deploymentName;

    podDisruptionBudget.new() +
    podDisruptionBudget.mixin.metadata.withName(pdbName) +
    podDisruptionBudget.mixin.metadata.withLabels({ name: pdbName }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ name: deploymentName }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(maxUnavailable),

  newIngesterPdb(ingesterName)::
    $.newLokiPdb(ingesterName),

  newLokiStatefulSet(name, replicas, container, pvc, podManagementPolicy='Parallel')::
    local statefulSet = $.apps.v1.statefulSet;

    statefulSet.new(name, replicas, [container], pvc) +
    statefulSet.mixin.spec.withServiceName(name) +
    // statefulSet.mixin.metadata.withNamespace($._config.namespace) +
    // statefulSet.mixin.metadata.withLabels({ name: name }) +
    statefulSet.mixin.spec.template.metadata.withLabels({ name: name }) +
    statefulSet.mixin.spec.selector.withMatchLabels({ name: name }) +
    // statefulSet.mixin.spec.template.spec.securityContext.withRunAsUser(0) +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    $.config_hash_mixin +
    (if podManagementPolicy != null then statefulSet.mixin.spec.withPodManagementPolicy(podManagementPolicy) else {}) +
    (if !std.isObject($._config.node_selector) then {} else statefulSet.mixin.spec.template.spec.withNodeSelectorMixin($._config.node_selector)) +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ),
}
