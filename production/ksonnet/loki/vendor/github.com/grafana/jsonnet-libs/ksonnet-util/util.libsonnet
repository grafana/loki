// util.libsonnet provides a number of useful (opinionated) shortcuts to replace boilerplate code

local util(k) = {
  // checkFlagsMap checks map for presence of flags that values with both with and without prefix set ('foo' and '-foo').
  checkFlagsMap(map, prefix): [
    error 'key "%(key)s" provided with value "%(value)s" but key "%(prefix)s%(key)s" was provided too with value "%(otherValue)s", if want to ignore this, set check=false in mapToFlags' %
          { key: key, value: map[key], prefix: prefix, otherValue: map[prefix + key] }
    for key in std.objectFields(map)
    if map[key] != null && std.objectHas(map, prefix + key) && map[prefix + key] != null
  ],

  // mapToFlags converts a map to a set of golang-style command line flags.
  // if check=true, it will check for 'foo' and '-foo' presence, failing in that case.
  mapToFlags(map, prefix='-', check=true): [
    '%s%s=%s' % [prefix, key, map[key]]
    for key in std.objectFields(map)
    if map[key] != null
  ] + if check then $.checkFlagsMap(map, prefix) else [],

  // serviceFor create service for a given deployment.
  serviceFor(deployment, ignored_labels=[], nameFormat='%(container)s-%(port)s')::
    local container = k.core.v1.container;
    local service = k.core.v1.service;
    local servicePort = k.core.v1.servicePort;
    local ports = [
      servicePort.newNamed(
        name=(nameFormat % { container: c.name, port: port.name }),
        port=port.containerPort,
        targetPort=port.containerPort
      ) +
      if std.objectHas(port, 'protocol')
      then servicePort.withProtocol(port.protocol)
      else {}
      for c in deployment.spec.template.spec.containers
      for port in (c + container.withPortsMixin([])).ports
    ];
    local labels = {
      [x]: deployment.spec.template.metadata.labels[x]
      for x in std.objectFields(deployment.spec.template.metadata.labels)
      if std.count(ignored_labels, x) == 0
    };

    service.new(
      deployment.metadata.name,  // name
      labels,  // selector
      ports,
    ) +
    service.mixin.metadata.withLabels({ name: deployment.metadata.name }),

  // rbac creates a service account, role and role binding with the given
  // name and rules.
  rbac(name, rules, namespace):: {
    local clusterRole = k.rbac.v1.clusterRole,
    local clusterRoleBinding = k.rbac.v1.clusterRoleBinding,
    local subject = k.rbac.v1.subject,
    local serviceAccount = k.core.v1.serviceAccount,

    service_account:
      serviceAccount.new(name),

    cluster_role:
      clusterRole.new() +
      clusterRole.mixin.metadata.withName(name) +
      clusterRole.withRules(rules),

    cluster_role_binding:
      clusterRoleBinding.new() +
      clusterRoleBinding.mixin.metadata.withName(name) +
      clusterRoleBinding.mixin.roleRef.withApiGroup('rbac.authorization.k8s.io') +
      clusterRoleBinding.mixin.roleRef.withKind('ClusterRole') +
      clusterRoleBinding.mixin.roleRef.withName(name) +
      clusterRoleBinding.withSubjects([
        subject.new() +
        subject.withKind('ServiceAccount') +
        subject.withName(name) +
        subject.withNamespace(namespace),
      ]),
  },

  namespacedRBAC(name, rules, namespace):: {
    local role = k.rbac.v1.role,
    local roleBinding = k.rbac.v1.roleBinding,
    local subject = k.rbac.v1.subject,
    local serviceAccount = k.core.v1.serviceAccount,

    service_account:
      serviceAccount.new(name) +
      serviceAccount.mixin.metadata.withNamespace(namespace),

    role:
      role.new() +
      role.mixin.metadata.withName(name) +
      role.mixin.metadata.withNamespace(namespace) +
      role.withRules(rules),

    role_binding:
      roleBinding.new() +
      roleBinding.mixin.metadata.withName(name) +
      roleBinding.mixin.metadata.withNamespace(namespace) +
      roleBinding.mixin.roleRef.withApiGroup('rbac.authorization.k8s.io') +
      roleBinding.mixin.roleRef.withKind('Role') +
      roleBinding.mixin.roleRef.withName(name) +
      roleBinding.withSubjects([
        subject.new() +
        subject.withKind('ServiceAccount') +
        subject.withName(name) +
        subject.withNamespace(namespace),
      ]),
  },

  // VolumeMount helper functions can be augmented with mixins.
  // For example, passing "volumeMount.withSubPath(subpath)" will result in
  // a subpath mixin.
  configVolumeMount(name, path, volumeMountMixin={})::
    $.volumeMounts([$.volumeMountItem(name, path, volumeMountMixin)]),

  // configMapVolumeMount adds a configMap to deployment-like objects.
  // It will also add an annotation hash to ensure the pods are re-deployed
  // when the config map changes.
  configMapVolumeMount(configMap, path, volumeMountMixin={})::
    $.volumeMounts([$.configMapVolumeMountItem(configMap, path, volumeMountMixin)]),


  // configMapVolumeMountItem represents a config map to be mounted.
  // It is used in the volumeMounts function
  configMapVolumeMountItem(configMap, path, volumeMountMixin={})::
    local name = configMap.metadata.name;
    local annotations = { ['%s-hash' % name]: std.md5(std.toString(configMap)) };
    $.volumeMountItem(name, path, volumeMountMixin, annotations),

  // volumeMountItem represents a volume to be mounted.
  // It is used in the volumeMounts function
  volumeMountItem(name, path, volumeMountMixin={}, annotations={}):: {
    name: name,
    path: path,
    volumeMountMixin: volumeMountMixin,
    annotations: annotations,
  },

  // volumeMounts adds an array of volumeMountItem to deployment-like objects.
  // It can also add a set of annotations for each mount
  volumeMounts(mounts)::
    local container = k.core.v1.container,
          deployment = k.apps.v1.deployment,
          volumeMount = k.core.v1.volumeMount,
          volume = k.core.v1.volume;
    local addMounts(c) = c + container.withVolumeMountsMixin([
      volumeMount.new(m.name, m.path) +
      m.volumeMountMixin
      for m in mounts
    ]);
    local annotations = std.foldl(
      function(acc, ann) acc + ann,
      [m.annotations for m in mounts],
      {}
    );

    deployment.mapContainers(addMounts)
    + deployment.mixin.spec.template.spec.withVolumesMixin([
      volume.fromConfigMap(m.name, m.name)
      for m in mounts
    ])
    + (if annotations != {} then deployment.mixin.spec.template.metadata.withAnnotationsMixin(annotations) else {}),

  hostVolumeMount(name, hostPath, path, readOnly=false, volumeMountMixin={}, volumeMixin={})::
    local container = k.core.v1.container,
          deployment = k.apps.v1.deployment,
          volumeMount = k.core.v1.volumeMount,
          volume = k.core.v1.volume,
          addMount(c) = c + container.withVolumeMountsMixin(
      volumeMount.new(name, path, readOnly=readOnly) +
      volumeMountMixin,
    );

    deployment.mapContainers(addMount) +
    deployment.mixin.spec.template.spec.withVolumesMixin([
      volume.fromHostPath(name, hostPath) +
      volumeMixin,
    ]),

  pvcVolumeMount(pvcName, path, readOnly=false, volumeMountMixin={})::
    local container = k.core.v1.container,
          deployment = k.apps.v1.deployment,
          volumeMount = k.core.v1.volumeMount,
          volume = k.core.v1.volume,
          addMount(c) = c + container.withVolumeMountsMixin(
      volumeMount.new(pvcName, path, readOnly=readOnly) +
      volumeMountMixin,
    );

    deployment.mapContainers(addMount) +
    deployment.mixin.spec.template.spec.withVolumesMixin([
      volume.fromPersistentVolumeClaim(pvcName, pvcName),
    ]),

  secretVolumeMount(name, path, defaultMode=256, volumeMountMixin={})::
    local container = k.core.v1.container,
          deployment = k.apps.v1.deployment,
          volumeMount = k.core.v1.volumeMount,
          volume = k.core.v1.volume,
          addMount(c) = c + container.withVolumeMountsMixin(
      volumeMount.new(name, path) +
      volumeMountMixin,
    );

    deployment.mapContainers(addMount) +
    deployment.mixin.spec.template.spec.withVolumesMixin([
      volume.fromSecret(name, secretName=name) +
      volume.mixin.secret.withDefaultMode(defaultMode),
    ]),

  emptyVolumeMount(name, path, volumeMountMixin={}, volumeMixin={})::
    local container = k.core.v1.container,
          deployment = k.apps.v1.deployment,
          volumeMount = k.core.v1.volumeMount,
          volume = k.core.v1.volume,
          addMount(c) = c + container.withVolumeMountsMixin(
      volumeMount.new(name, path) +
      volumeMountMixin,
    );

    deployment.mapContainers(addMount) +
    deployment.mixin.spec.template.spec.withVolumesMixin([
      volume.fromEmptyDir(name) + volumeMixin,
    ]),

  manifestYaml(value):: (
    local f = std.native('manifestYamlFromJson');
    if f != null
    then f(std.toString(value))
    else std.manifestYamlDoc(value)
  ),

  resourcesRequests(cpu, memory)::
    k.core.v1.container.mixin.resources.withRequests(
      (if cpu != null
       then { cpu: cpu }
       else {}) +
      (if memory != null
       then { memory: memory }
       else {})
    ),

  resourcesRequestsMixin(cpu, memory)::
    k.core.v1.container.mixin.resources.withRequestsMixin(
      (if cpu != null
       then { cpu: cpu }
       else {}) +
      (if memory != null
       then { memory: memory }
       else {})
    ),

  resourcesLimits(cpu, memory)::
    k.core.v1.container.mixin.resources.withLimits(
      (if cpu != null
       then { cpu: cpu }
       else {}) +
      (if memory != null
       then { memory: memory }
       else {})
    ),

  resourcesLimitsMixin(cpu, memory)::
    k.core.v1.container.mixin.resources.withLimitsMixin(
      (if cpu != null
       then { cpu: cpu }
       else {}) +
      (if memory != null
       then { memory: memory }
       else {})
    ),

  antiAffinity:
    {
      local deployment = k.apps.v1.deployment,
      local podAntiAffinity = deployment.mixin.spec.template.spec.affinity.podAntiAffinity,
      local name = super.spec.template.metadata.labels.name,

      spec+: podAntiAffinity.withRequiredDuringSchedulingIgnoredDuringExecution([
        podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.new() +
        podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.mixin.labelSelector.withMatchLabels({ name: name }) +
        podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.withTopologyKey('kubernetes.io/hostname'),
      ]).spec,
    },

  antiAffinityStatefulSet:
    {
      local statefulSet = k.apps.v1.statefulSet,
      local podAntiAffinity = statefulSet.mixin.spec.template.spec.affinity.podAntiAffinity,
      local name = super.spec.template.metadata.labels.name,

      spec+: podAntiAffinity.withRequiredDuringSchedulingIgnoredDuringExecution([
        podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.new() +
        podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.mixin.labelSelector.withMatchLabels({ name: name }) +
        podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.withTopologyKey('kubernetes.io/hostname'),
      ]).spec,
    },

  // Add a priority to the pods in a deployment (or deployment-like objects
  // such as a statefulset).
  local deployment = k.apps.v1.deployment,
  podPriority(p):
    deployment.mixin.spec.template.spec.withPriorityClassName(p),
};

util((import 'grafana.libsonnet')) + {
  withK(k):: util(k),
}
