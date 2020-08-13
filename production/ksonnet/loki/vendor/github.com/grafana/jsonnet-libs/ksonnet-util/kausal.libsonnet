// Override defaults paramters for objects in the ksonnet libs here.
local k = import 'k.libsonnet';

k {
  _config+:: {
    enable_rbac: true,
    enable_pod_priorities: false,
    namespace: error 'Must define a namespace',
  },

  core+: {
    v1+: {
      configMap+: {
        new(name)::
          super.new(name, {}),
        withData(data)::
          if (data == {}) then {}
          else super.withData(data),
        withDataMixin(data)::
          if (data == {}) then {}
          else super.withDataMixin(data),
      },

      // Expose containerPort type.
      containerPort:: $.core.v1.container.portsType {
        // Force all ports to have names.
        new(name, port)::
          super.newNamed(name=name, containerPort=port),

        // Shortcut constructor for UDP ports.
        newUDP(name, port)::
          super.newNamed(name=name, containerPort=port) +
          super.withProtocol('UDP'),
      },

      // Expose volumes type.
      volume:: $.core.v1.pod.mixin.spec.volumesType {
        // Make items parameter optional from fromConfigMap
        fromConfigMap(name, configMapName, configMapItems=[])::
          {
            configMap+:
              if configMapItems == [] then { items:: null }
              else {},
          }
          + super.fromConfigMap(name, configMapName, configMapItems),

        // Shortcut constructor for secret volumes.
        fromSecret(name, secretName)::
          super.withName(name) +
          super.mixin.secret.withSecretName(secretName),

        // Rename emptyDir to claimName
        fromPersistentVolumeClaim(name='', claimName=''):: super.fromPersistentVolumeClaim(name=name, emptyDir=claimName),
      },

      volumeMount:: $.core.v1.container.volumeMountsType {
        // Override new, such that it doesn't always set readOnly: false.
        new(name, mountPath, readOnly=false)::
          {} + self.withName(name) + self.withMountPath(mountPath) +
          if readOnly
          then self.withReadOnly(readOnly)
          else {},
      },

      persistentVolumeClaim+:: {
        new(name='')::
          if name != '' then
            if 'new' in super
            then super.new(name)
            else
              {} + super.mixin.metadata.withName(name)
          else
            {},
      },

      container:: $.apps.v1.deployment.mixin.spec.template.spec.containersType {
        new(name, image)::
          super.new(name, image) +
          super.withImagePullPolicy('IfNotPresent'),

        withEnvMixin(es)::
          // if an envvar has an empty value ("") we want to remove that property
          // because k8s will remove that and then it would always
          // show up as a difference.
          local removeEmptyValue(obj) =
            if std.objectHas(obj, 'value') && std.length(obj.value) == 0 then
              {
                [k]: obj[k]
                for k in std.objectFields(obj)
                if k != 'value'
              }
            else
              obj;
          super.withEnvMixin([
            removeEmptyValue(envvar)
            for envvar in es
          ]),

        withEnvMap(es)::
          self.withEnvMixin([
            $.core.v1.container.envType.new(k, es[k])
            for k in std.objectFields(es)
          ]),
      },

      toleration:: $.apps.v1.deployment.mixin.spec.template.spec.tolerationsType,

      servicePort:: $.core.v1.service.mixin.spec.portsType,
    },
  },

  batch+: {
    v1beta1+: {
      cronJob+: {
        new(name='', schedule='', containers=[])::
          super.new() +
          (
            if name != '' then
              super.mixin.metadata.withName(name) +
              // set name label on pod
              super.mixin.spec.jobTemplate.spec.template.metadata.withLabels({ name: name })
            else
              {}
          ) +
          (
            if schedule != '' then
              super.mixin.spec.withSchedule(schedule)
            else
              {}
          ) +
          super.mixin.spec.jobTemplate.spec.template.spec.withContainers(containers),
      },
    },
  },

  local appsExtentions = {
    daemonSet+: {
      new(name, containers, podLabels={})::
        local labels = podLabels { name: name };

        super.new() +
        super.mixin.metadata.withName(name) +
        super.mixin.spec.template.metadata.withLabels(labels) +
        super.mixin.spec.template.spec.withContainers(containers) +

        // Can't think of a reason we wouldn't want a DaemonSet to run on
        // every node.
        super.mixin.spec.template.spec.withTolerations([
          $.core.v1.toleration.new() +
          $.core.v1.toleration.withOperator('Exists') +
          $.core.v1.toleration.withEffect('NoSchedule'),
        ]) +

        // We want to specify a minReadySeconds on every deamonset, so we get some
        // very basic canarying, for instance, with bad arguments.
        super.mixin.spec.withMinReadySeconds(10) +
        super.mixin.spec.updateStrategy.withType('RollingUpdate') +

        // apps.v1 requires an explicit selector:
        super.mixin.spec.selector.withMatchLabels(labels),
    },

    deployment+: {
      new(name, replicas, containers, podLabels={})::
        local labels = podLabels { name: name };

        super.new(name, replicas, containers, labels) +

        // We want to specify a minReadySeconds on every deployment, so we get some
        // very basic canarying, for instance, with bad arguments.
        super.mixin.spec.withMinReadySeconds(10) +

        // We want to add a sensible default for the number of old deployments
        // handing around.
        super.mixin.spec.withRevisionHistoryLimit(10) +

        // apps.v1 requires an explicit selector:
        super.mixin.spec.selector.withMatchLabels(labels),
    },

    statefulSet+: {
      new(name, replicas, containers, volumeClaims, podLabels={})::
        local labels = podLabels { name: name };

        super.new(name, replicas, containers, volumeClaims, labels) +
        super.mixin.spec.updateStrategy.withType('RollingUpdate') +

        // apps.v1 requires an explicit selector:
        super.mixin.spec.selector.withMatchLabels(labels) +

        // remove volumeClaimTemplates if empty (otherwise it will create a diff all the time)
        (if std.length(volumeClaims) == 0 then {
           spec+: { volumeClaimTemplates:: {} },
         } else {}),
    },
  },

  extensions+: {
    v1beta1+: appsExtentions,
  },

  apps+: {
    v1beta1+: appsExtentions,
    v1+: appsExtentions,
  },

  rbac+: {
    v1beta1+: {
      // Shortcut to access the hidden types.
      policyRule:: $.rbac.v1beta1.clusterRole.rulesType,
      subject:: $.rbac.v1beta1.clusterRoleBinding.subjectsType,
    },
  },

  util+:: {
    // mapToFlags converts a map to a set of golang-style command line flags.
    mapToFlags(map, prefix='-'): [
      '%s%s=%s' % [prefix, key, map[key]]
      for key in std.objectFields(map)
      if map[key] != null
    ],

    // serviceFor create service for a given deployment.
    serviceFor(deployment, ignored_labels=[], nameFormat='%(container)s-%(port)s')::
      local container = $.core.v1.container;
      local service = $.core.v1.service;
      local servicePort = service.mixin.spec.portsType;
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
    rbac(name, rules)::
      if $._config.enable_rbac
      then {
        local clusterRole = $.rbac.v1beta1.clusterRole,
        local clusterRoleBinding = $.rbac.v1beta1.clusterRoleBinding,
        local subject = $.rbac.v1beta1.subject,
        local serviceAccount = $.core.v1.serviceAccount,

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
            subject.withNamespace($._config.namespace),
          ]),
      }
      else {},

    namespacedRBAC(name, rules)::
      if $._config.enable_rbac
      then {
        local role = $.rbac.v1beta1.role,
        local roleBinding = $.rbac.v1beta1.roleBinding,
        local subject = $.rbac.v1beta1.subject,
        local serviceAccount = $.core.v1.serviceAccount,

        service_account:
          serviceAccount.new(name) +
          serviceAccount.mixin.metadata.withNamespace($._config.namespace),

        role:
          role.new() +
          role.mixin.metadata.withName(name) +
          role.mixin.metadata.withNamespace($._config.namespace) +
          role.withRules(rules),

        cluster_role_binding:
          roleBinding.new() +
          roleBinding.mixin.metadata.withName(name) +
          roleBinding.mixin.metadata.withNamespace($._config.namespace) +
          roleBinding.mixin.roleRef.withApiGroup('rbac.authorization.k8s.io') +
          roleBinding.mixin.roleRef.withKind('Role') +
          roleBinding.mixin.roleRef.withName(name) +
          roleBinding.withSubjects([
            subject.new() +
            subject.withKind('ServiceAccount') +
            subject.withName(name) +
            subject.withNamespace($._config.namespace),
          ]),
      }
      else {},

    // VolumeMount helper functions can be augmented with mixins.
    // For example, passing "volumeMount.withSubPath(subpath)" will result in
    // a subpath mixin.
    configVolumeMount(name, path, volumeMountMixin={})::
      local container = $.core.v1.container,
            deployment = $.apps.v1.deployment,
            volumeMount = $.core.v1.volumeMount,
            volume = $.core.v1.volume,
            addMount(c) = c + container.withVolumeMountsMixin(
        volumeMount.new(name, path) +
        volumeMountMixin,
      );

      deployment.mapContainers(addMount) +
      deployment.mixin.spec.template.spec.withVolumesMixin([
        volume.fromConfigMap(name, name),
      ]),

    // configMapVolumeMount adds a configMap to deployment-like objects.
    // It will also add an annotation hash to ensure the pods are re-deployed
    // when the config map changes.
    configMapVolumeMount(configMap, path, volumeMountMixin={})::
      local name = configMap.metadata.name,
            hash = std.md5(std.toString(configMap)),
            container = $.core.v1.container,
            deployment = $.apps.v1.deployment,
            volumeMount = $.core.v1.volumeMount,
            volume = $.core.v1.volume,
            addMount(c) = c + container.withVolumeMountsMixin(
        volumeMount.new(name, path) +
        volumeMountMixin,
      );

      deployment.mapContainers(addMount) +
      deployment.mixin.spec.template.spec.withVolumesMixin([
        volume.fromConfigMap(name, name),
      ]) +
      deployment.mixin.spec.template.metadata.withAnnotationsMixin({
        ['%s-hash' % name]: hash,
      }),

    hostVolumeMount(name, hostPath, path, readOnly=false, volumeMountMixin={})::
      local container = $.core.v1.container,
            deployment = $.apps.v1.deployment,
            volumeMount = $.core.v1.volumeMount,
            volume = $.core.v1.volume,
            addMount(c) = c + container.withVolumeMountsMixin(
        volumeMount.new(name, path, readOnly=readOnly) +
        volumeMountMixin,
      );

      deployment.mapContainers(addMount) +
      deployment.mixin.spec.template.spec.withVolumesMixin([
        volume.fromHostPath(name, hostPath),
      ]),

    secretVolumeMount(name, path, defaultMode=256, volumeMountMixin={})::
      local container = $.core.v1.container,
            deployment = $.apps.v1.deployment,
            volumeMount = $.core.v1.volumeMount,
            volume = $.core.v1.volume,
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
      local container = $.core.v1.container,
            deployment = $.apps.v1.deployment,
            volumeMount = $.core.v1.volumeMount,
            volume = $.core.v1.volume,
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
      f(std.toString(value))
    ),

    resourcesRequests(cpu, memory)::
      $.core.v1.container.mixin.resources.withRequests(
        (if cpu != null
         then { cpu: cpu }
         else {}) +
        (if memory != null
         then { memory: memory }
         else {})
      ),

    resourcesLimits(cpu, memory)::
      $.core.v1.container.mixin.resources.withLimits(
        (if cpu != null
         then { cpu: cpu }
         else {}) +
        (if memory != null
         then { memory: memory }
         else {})
      ),

    antiAffinity:
      {
        local deployment = $.apps.v1.deployment,
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
        local statefulSet = $.apps.v1.statefulSet,
        local podAntiAffinity = statefulSet.mixin.spec.template.spec.affinity.podAntiAffinity,
        local name = super.spec.template.metadata.labels.name,

        spec+: podAntiAffinity.withRequiredDuringSchedulingIgnoredDuringExecution([
          podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.new() +
          podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.mixin.labelSelector.withMatchLabels({ name: name }) +
          podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.withTopologyKey('kubernetes.io/hostname'),
        ]).spec,
      },

    // Add a priority to the pods in a deployment (or deployment-like objects
    // such as a statefulset) iff _config.enable_pod_priorities is set to true.
    podPriority(p):
      local deployment = $.apps.v1.deployment;
      if $._config.enable_pod_priorities
      then deployment.mixin.spec.template.spec.withPriorityClassName(p)
      else {},
  },
}
