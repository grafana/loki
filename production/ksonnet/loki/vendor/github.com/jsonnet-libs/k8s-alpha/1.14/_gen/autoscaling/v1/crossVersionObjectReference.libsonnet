{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='crossVersionObjectReference', url='', help='CrossVersionObjectReference contains enough information to let you identify the referred resource.'),
  '#withKind':: d.fn(help='Kind of the referent; More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds"', args=[d.arg(name='kind', type=d.T.string)]),
  withKind(kind): { kind: kind },
  '#withName':: d.fn(help='Name of the referent; More info: http://kubernetes.io/docs/user-guide/identifiers#names', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#mixin': 'ignore',
  mixin: self
}