{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='topologySelectorLabelRequirement', url='', help='A topology selector requirement is a selector that matches given label. This is an alpha feature and may change in the future.'),
  '#withKey':: d.fn(help='The label key that the selector applies to.', args=[d.arg(name='key', type=d.T.string)]),
  withKey(key): { key: key },
  '#withValues':: d.fn(help='An array of string values. One value must match the label to be selected. Each entry in Values is ORed.', args=[d.arg(name='values', type=d.T.array)]),
  withValues(values): { values: if std.isArray(v=values) then values else [values] },
  '#withValuesMixin':: d.fn(help='An array of string values. One value must match the label to be selected. Each entry in Values is ORed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='values', type=d.T.array)]),
  withValuesMixin(values): { values+: if std.isArray(v=values) then values else [values] },
  '#mixin': 'ignore',
  mixin: self
}