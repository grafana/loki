// grafana.libsonnet provides the k-compat layer with grafana-opinionated defaults
(import 'k-compat.libsonnet')
+ {
  core+: {
    v1+: {
      containerPort+:: {
        // Force all ports to have names.
        new(name, port)::
          super.newNamed(name=name, containerPort=port),

        // Shortcut constructor for UDP ports.
        newUDP(name, port)::
          super.newNamedUDP(name=name, containerPort=port),
      },

      container+:: {
        new(name, image)::
          super.new(name, image) +
          super.withImagePullPolicy('IfNotPresent'),
      },
    },
  },

  local appsExtentions = {
    daemonSet+: {
      new(name, containers, podLabels={})::
        super.new(name, containers, podLabels={}) +

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
        super.mixin.spec.updateStrategy.withType('RollingUpdate'),
    },

    deployment+: {
      new(name, replicas, containers, podLabels={})::
        super.new(name, replicas, containers, podLabels) +

        // We want to specify a minReadySeconds on every deployment, so we get some
        // very basic canarying, for instance, with bad arguments.
        super.mixin.spec.withMinReadySeconds(10) +

        // We want to add a sensible default for the number of old deployments
        // handing around.
        super.mixin.spec.withRevisionHistoryLimit(10),
    },

    statefulSet+: {
      new(name, replicas, containers, volumeClaims=[], podLabels={})::
        super.new(name, replicas, containers, volumeClaims, podLabels) +
        super.mixin.spec.updateStrategy.withType('RollingUpdate'),
    },
  },

  apps+: {
    v1beta1+: appsExtentions,
    v1+: appsExtentions,
  },
}
