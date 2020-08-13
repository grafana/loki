{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='typedLocalObjectReference', url='', help='TypedLocalObjectReference contains enough information to let you locate the typed referenced object inside the same namespace.'),
  '#withApiGroup':: d.fn(help='APIGroup is the group for the resource being referenced. If APIGroup is not specified, the specified Kind must be in the core API group. For any other third-party types, APIGroup is required.', args=[d.arg(name='apiGroup', type=d.T.string)]),
  withApiGroup(apiGroup): { apiGroup: apiGroup },
  '#withKind':: d.fn(help='Kind is the type of resource being referenced', args=[d.arg(name='kind', type=d.T.string)]),
  withKind(kind): { kind: kind },
  '#withName':: d.fn(help='Name is the name of resource being referenced', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#mixin': 'ignore',
  mixin: self
}