{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='secretReference', url='', help='SecretReference represents a Secret Reference. It has enough information to retrieve secret in any namespace'),
  '#withName':: d.fn(help='Name is unique within a namespace to reference a secret resource.', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withNamespace':: d.fn(help='Namespace defines the space within which the secret name must be unique.', args=[d.arg(name='namespace', type=d.T.string)]),
  withNamespace(namespace): { namespace: namespace },
  '#mixin': 'ignore',
  mixin: self
}