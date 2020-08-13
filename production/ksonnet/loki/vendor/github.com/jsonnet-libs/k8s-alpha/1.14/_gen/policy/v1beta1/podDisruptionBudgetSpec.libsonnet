{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='podDisruptionBudgetSpec', url='', help='PodDisruptionBudgetSpec is a description of a PodDisruptionBudget.'),
  '#selector':: d.obj(help='A label selector is a label query over a set of resources. The result of matchLabels and matchExpressions are ANDed. An empty label selector matches all objects. A null label selector matches no objects.'),
  selector: {
    '#withMatchExpressions':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressions(matchExpressions): { selector+: { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchExpressionsMixin':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressionsMixin(matchExpressions): { selector+: { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchLabels':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabels(matchLabels): { selector+: { matchLabels: matchLabels } },
    '#withMatchLabelsMixin':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabelsMixin(matchLabels): { selector+: { matchLabels+: matchLabels } }
  },
  '#withMaxUnavailable':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='maxUnavailable', type=d.T.string)]),
  withMaxUnavailable(maxUnavailable): { maxUnavailable: maxUnavailable },
  '#withMinAvailable':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='minAvailable', type=d.T.string)]),
  withMinAvailable(minAvailable): { minAvailable: minAvailable },
  '#mixin': 'ignore',
  mixin: self
}