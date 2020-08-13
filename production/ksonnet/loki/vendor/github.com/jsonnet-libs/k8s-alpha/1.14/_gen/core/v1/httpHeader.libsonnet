{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='httpHeader', url='', help='HTTPHeader describes a custom header to be used in HTTP probes'),
  '#withName':: d.fn(help='The header field name', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withValue':: d.fn(help='The header field value', args=[d.arg(name='value', type=d.T.string)]),
  withValue(value): { value: value },
  '#mixin': 'ignore',
  mixin: self
}