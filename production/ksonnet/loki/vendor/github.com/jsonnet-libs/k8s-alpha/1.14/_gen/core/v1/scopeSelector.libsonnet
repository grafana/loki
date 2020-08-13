{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='scopeSelector', url='', help='A scope selector represents the AND of the selectors represented by the scoped-resource selector requirements.'),
  '#withMatchExpressions':: d.fn(help='A list of scope selector requirements by scope of the resources.', args=[d.arg(name='matchExpressions', type=d.T.array)]),
  withMatchExpressions(matchExpressions): { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] },
  '#withMatchExpressionsMixin':: d.fn(help='A list of scope selector requirements by scope of the resources.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchExpressions', type=d.T.array)]),
  withMatchExpressionsMixin(matchExpressions): { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] },
  '#mixin': 'ignore',
  mixin: self
}