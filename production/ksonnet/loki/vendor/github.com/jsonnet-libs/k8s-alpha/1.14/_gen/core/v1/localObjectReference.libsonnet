{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='localObjectReference', url='', help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
  '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#mixin': 'ignore',
  mixin: self
}