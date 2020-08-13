{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='namespaceStatus', url='', help='NamespaceStatus is information about the current status of a Namespace.'),
  '#withPhase':: d.fn(help='Phase is the current lifecycle phase of the namespace. More info: https://kubernetes.io/docs/tasks/administer-cluster/namespaces/', args=[d.arg(name='phase', type=d.T.string)]),
  withPhase(phase): { phase: phase },
  '#mixin': 'ignore',
  mixin: self
}