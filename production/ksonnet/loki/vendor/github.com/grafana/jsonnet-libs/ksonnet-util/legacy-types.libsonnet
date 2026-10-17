// legacy-types.libsonnet exposes hidden types from ksonnet-lib as first class citizens
// This list is likely to be incomplete.
{
  core+: {
    v1+: {
      container:: $.apps.v1.deployment.mixin.spec.template.spec.containersType,
      containerPort:: $.core.v1.container.portsType,
      envVar:: $.core.v1.container.envType,
      envFromSource:: $.core.v1.container.envFromType,
      servicePort:: $.core.v1.service.mixin.spec.portsType,
      toleration:: $.apps.v1.deployment.mixin.spec.template.spec.tolerationsType,
      volume:: $.core.v1.pod.mixin.spec.volumesType,
      volumeMount:: $.core.v1.container.volumeMountsType,
    },
  },
  rbac+: {
    v1+: {
      policyRule:: $.rbac.v1.clusterRole.rulesType,
      subject:: $.rbac.v1.clusterRoleBinding.subjectsType,
    },
    // TODO: the v1beta1 RBAC API has been removed in Kubernetes 1.22 and should get removed once 1.22 is the oldest supported version
    v1beta1+: {
      policyRule:: $.rbac.v1.clusterRole.rulesType,
      subject:: $.rbac.v1.clusterRoleBinding.subjectsType,
    },
  },
}
