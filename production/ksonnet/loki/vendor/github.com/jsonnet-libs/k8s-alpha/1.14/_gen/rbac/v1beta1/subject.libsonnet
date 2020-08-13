{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='subject', url='', help='Subject contains a reference to the object or user identities a role binding applies to.  This can either hold a direct API object reference, or a value for non-objects such as user and group names.'),
  '#withApiGroup':: d.fn(help='APIGroup holds the API group of the referenced subject. Defaults to "" for ServiceAccount subjects. Defaults to "rbac.authorization.k8s.io" for User and Group subjects.', args=[d.arg(name='apiGroup', type=d.T.string)]),
  withApiGroup(apiGroup): { apiGroup: apiGroup },
  '#withKind':: d.fn(help='Kind of object being referenced. Values defined by this API group are "User", "Group", and "ServiceAccount". If the Authorizer does not recognized the kind value, the Authorizer should report an error.', args=[d.arg(name='kind', type=d.T.string)]),
  withKind(kind): { kind: kind },
  '#withName':: d.fn(help='Name of the object being referenced.', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withNamespace':: d.fn(help='Namespace of the referenced object.  If the object kind is non-namespace, such as "User" or "Group", and this value is not empty the Authorizer should report an error.', args=[d.arg(name='namespace', type=d.T.string)]),
  withNamespace(namespace): { namespace: namespace },
  '#mixin': 'ignore',
  mixin: self
}