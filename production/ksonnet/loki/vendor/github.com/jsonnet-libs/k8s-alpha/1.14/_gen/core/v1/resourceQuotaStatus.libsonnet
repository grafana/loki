{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='resourceQuotaStatus', url='', help='ResourceQuotaStatus defines the enforced hard limits and observed use.'),
  '#withHard':: d.fn(help='Hard is the set of enforced hard limits for each named resource. More info: https://kubernetes.io/docs/concepts/policy/resource-quotas/', args=[d.arg(name='hard', type=d.T.object)]),
  withHard(hard): { hard: hard },
  '#withHardMixin':: d.fn(help='Hard is the set of enforced hard limits for each named resource. More info: https://kubernetes.io/docs/concepts/policy/resource-quotas/\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='hard', type=d.T.object)]),
  withHardMixin(hard): { hard+: hard },
  '#withUsed':: d.fn(help='Used is the current observed total usage of the resource in the namespace.', args=[d.arg(name='used', type=d.T.object)]),
  withUsed(used): { used: used },
  '#withUsedMixin':: d.fn(help='Used is the current observed total usage of the resource in the namespace.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='used', type=d.T.object)]),
  withUsedMixin(used): { used+: used },
  '#mixin': 'ignore',
  mixin: self
}