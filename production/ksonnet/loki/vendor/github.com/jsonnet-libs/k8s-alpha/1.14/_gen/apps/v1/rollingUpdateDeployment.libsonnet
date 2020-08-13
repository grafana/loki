{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='rollingUpdateDeployment', url='', help='Spec to control the desired behavior of rolling update.'),
  '#withMaxSurge':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='maxSurge', type=d.T.string)]),
  withMaxSurge(maxSurge): { maxSurge: maxSurge },
  '#withMaxUnavailable':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='maxUnavailable', type=d.T.string)]),
  withMaxUnavailable(maxUnavailable): { maxUnavailable: maxUnavailable },
  '#mixin': 'ignore',
  mixin: self
}