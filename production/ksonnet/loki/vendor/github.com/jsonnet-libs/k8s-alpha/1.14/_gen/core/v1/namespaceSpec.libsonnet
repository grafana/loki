{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='namespaceSpec', url='', help='NamespaceSpec describes the attributes on a Namespace.'),
  '#withFinalizers':: d.fn(help='Finalizers is an opaque list of values that must be empty to permanently remove object from storage. More info: https://kubernetes.io/docs/tasks/administer-cluster/namespaces/', args=[d.arg(name='finalizers', type=d.T.array)]),
  withFinalizers(finalizers): { finalizers: if std.isArray(v=finalizers) then finalizers else [finalizers] },
  '#withFinalizersMixin':: d.fn(help='Finalizers is an opaque list of values that must be empty to permanently remove object from storage. More info: https://kubernetes.io/docs/tasks/administer-cluster/namespaces/\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='finalizers', type=d.T.array)]),
  withFinalizersMixin(finalizers): { finalizers+: if std.isArray(v=finalizers) then finalizers else [finalizers] },
  '#mixin': 'ignore',
  mixin: self
}