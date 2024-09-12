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

    // parseCPU is used for conversion of Kubernetes CPU units to the corresponding float value of CPU cores.
    // Moreover, the function assumes the input is in a correct Kubernetes format, i.e., an integer, a float,
    // a string representation of an integer or a float, or a string containing a number ending with 'm'
    // representing a number of millicores.
    // Examples:
    // parseCPU(10) = parseCPU("10") = 10
    // parseCPU(4.5) = parse("4.5") = 4.5
    // parseCPU("3000m") = 3000 / 1000
    // parseCPU("3580m") = 3580 / 1000
    // parseCPU("3980.7m") = 3980.7 / 1000
    // parseCPU(0.5) = parse("0.5") = parse("500m") = 0.5
    parseCPU(v)::
      if std.isString(v) && std.endsWith(v, 'm') then std.parseJson(std.rstripChars(v, 'm')) / 1000
      else if std.isString(v) then std.parseJson(v)
      else if std.isNumber(v) then v
      else 0,

    // siToBytes is used to convert Kubernetes byte units to bytes.
    // Only works for limited set of SI prefixes: Ki, Mi, Gi, Ti.
    siToBytes(str):: (
      // Utility converting the input to a (potentially decimal) number of bytes
      local siToBytesDecimal(str) = (
        if std.endsWith(str, 'Ki') then (
          std.parseJson(std.rstripChars(str, 'Ki')) * std.pow(2, 10)
        ) else if std.endsWith(str, 'Mi') then (
          std.parseJson(std.rstripChars(str, 'Mi')) * std.pow(2, 20)
        ) else if std.endsWith(str, 'Gi') then (
          std.parseJson(std.rstripChars(str, 'Gi')) * std.pow(2, 30)
        ) else if std.endsWith(str, 'Ti') then (
          std.parseJson(std.rstripChars(str, 'Ti')) * std.pow(2, 40)
        ) else (
          std.parseJson(str)
        )
      );

      // Round down to nearest integer
      std.floor(siToBytesDecimal(str))
    ),
  },

  // functions for k8s objects
  newLokiPdb(deploymentName, maxUnavailable=1)::
    local podDisruptionBudget = $.policy.v1.podDisruptionBudget;
    local pdbName = '%s-pdb' % deploymentName;

    podDisruptionBudget.new(pdbName) +
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
