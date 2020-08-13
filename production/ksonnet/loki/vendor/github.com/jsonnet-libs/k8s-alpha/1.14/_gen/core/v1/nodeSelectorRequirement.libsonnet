{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeSelectorRequirement', url='', help='A node selector requirement is a selector that contains values, a key, and an operator that relates the key and values.'),
  '#withKey':: d.fn(help='The label key that the selector applies to.', args=[d.arg(name='key', type=d.T.string)]),
  withKey(key): { key: key },
  '#withOperator':: d.fn(help="Represents a key's relationship to a set of values. Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.", args=[d.arg(name='operator', type=d.T.string)]),
  withOperator(operator): { operator: operator },
  '#withValues':: d.fn(help='An array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. If the operator is Gt or Lt, the values array must have a single element, which will be interpreted as an integer. This array is replaced during a strategic merge patch.', args=[d.arg(name='values', type=d.T.array)]),
  withValues(values): { values: if std.isArray(v=values) then values else [values] },
  '#withValuesMixin':: d.fn(help='An array of string values. If the operator is In or NotIn, the values array must be non-empty. If the operator is Exists or DoesNotExist, the values array must be empty. If the operator is Gt or Lt, the values array must have a single element, which will be interpreted as an integer. This array is replaced during a strategic merge patch.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='values', type=d.T.array)]),
  withValuesMixin(values): { values+: if std.isArray(v=values) then values else [values] },
  '#mixin': 'ignore',
  mixin: self
}