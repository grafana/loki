local d = import 'doc-util/main.libsonnet';

local patch = {
  deployment+: {
    '#new'+: d.func.withArgs([
      d.arg('name', d.T.string),
      d.arg('replicas', d.T.int, 1),
      d.arg('containers', d.T.array),
      d.arg('podLabels', d.T.object, { app: 'name' }),
    ]),
    new(
      name,
      replicas=1,
      containers=error 'containers unset',
      podLabels={ app: 'name' },  // <- This is weird, but ksonnet did it
    ):: super.new(name)
        + super.spec.withReplicas(replicas)
        + super.spec.template.spec.withContainers(containers)
        + super.spec.template.metadata.withLabels(podLabels),
  },

  statefulSet+: {
    '#new'+: d.func.withArgs([
      d.arg('name', d.T.string),
      d.arg('replicas', d.T.int, 1),
      d.arg('containers', d.T.array),
      d.arg('volumeClaims', d.T.array, []),
      d.arg('podLabels', d.T.object, { app: 'name' }),
    ]),
    new(
      name,
      replicas=1,
      containers=error 'containers unset',
      volumeClaims=[],
      podLabels={ app: 'name' },  // <- This is weird, but ksonnet did it
    ):: super.new(name)
        + super.spec.withReplicas(replicas)
        + super.spec.template.spec.withContainers(containers)
        + super.spec.template.metadata.withLabels(podLabels)
        + super.spec.withVolumeClaimTemplates(volumeClaims),
  },
};

{
  extensions+: {  // old, remove asap
    v1beta1+: patch,
  },
  apps+: {
    v1+: patch,
    v1beta1+: patch,
    v1beta2+: patch,
  },
}
