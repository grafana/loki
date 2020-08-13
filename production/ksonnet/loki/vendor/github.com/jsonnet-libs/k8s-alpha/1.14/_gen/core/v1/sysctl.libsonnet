{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='sysctl', url='', help='Sysctl defines a kernel parameter to be set'),
  '#withName':: d.fn(help='Name of a property to set', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withValue':: d.fn(help='Value of a property to set', args=[d.arg(name='value', type=d.T.string)]),
  withValue(value): { value: value },
  '#mixin': 'ignore',
  mixin: self
}