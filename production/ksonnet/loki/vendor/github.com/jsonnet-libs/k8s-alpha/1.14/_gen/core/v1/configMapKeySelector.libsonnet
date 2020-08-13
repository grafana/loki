{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='configMapKeySelector', url='', help='Selects a key from a ConfigMap.'),
  '#withKey':: d.fn(help='The key to select.', args=[d.arg(name='key', type=d.T.string)]),
  withKey(key): { key: key },
  '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withOptional':: d.fn(help="Specify whether the ConfigMap or it's key must be defined", args=[d.arg(name='optional', type=d.T.boolean)]),
  withOptional(optional): { optional: optional },
  '#mixin': 'ignore',
  mixin: self
}