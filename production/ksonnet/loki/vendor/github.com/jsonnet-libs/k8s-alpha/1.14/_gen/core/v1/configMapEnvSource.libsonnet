{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='configMapEnvSource', url='', help="ConfigMapEnvSource selects a ConfigMap to populate the environment variables with.\n\nThe contents of the target ConfigMap's Data field will represent the key-value pairs as environment variables."),
  '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withOptional':: d.fn(help='Specify whether the ConfigMap must be defined', args=[d.arg(name='optional', type=d.T.boolean)]),
  withOptional(optional): { optional: optional },
  '#mixin': 'ignore',
  mixin: self
}