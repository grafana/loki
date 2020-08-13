{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='deploymentStrategy', url='', help='DeploymentStrategy describes how to replace existing pods with new ones.'),
  '#rollingUpdate':: d.obj(help='Spec to control the desired behavior of rolling update.'),
  rollingUpdate: {
    '#withMaxSurge':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='maxSurge', type=d.T.string)]),
    withMaxSurge(maxSurge): { rollingUpdate+: { maxSurge: maxSurge } },
    '#withMaxUnavailable':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='maxUnavailable', type=d.T.string)]),
    withMaxUnavailable(maxUnavailable): { rollingUpdate+: { maxUnavailable: maxUnavailable } }
  },
  '#withType':: d.fn(help='Type of deployment. Can be "Recreate" or "RollingUpdate". Default is RollingUpdate.', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}