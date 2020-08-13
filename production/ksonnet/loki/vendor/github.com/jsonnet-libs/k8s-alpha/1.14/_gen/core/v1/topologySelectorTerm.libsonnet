{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='topologySelectorTerm', url='', help='A topology selector term represents the result of label queries. A null or empty topology selector term matches no objects. The requirements of them are ANDed. It provides a subset of functionality as NodeSelectorTerm. This is an alpha feature and may change in the future.'),
  '#withMatchLabelExpressions':: d.fn(help='A list of topology selector requirements by labels.', args=[d.arg(name='matchLabelExpressions', type=d.T.array)]),
  withMatchLabelExpressions(matchLabelExpressions): { matchLabelExpressions: if std.isArray(v=matchLabelExpressions) then matchLabelExpressions else [matchLabelExpressions] },
  '#withMatchLabelExpressionsMixin':: d.fn(help='A list of topology selector requirements by labels.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchLabelExpressions', type=d.T.array)]),
  withMatchLabelExpressionsMixin(matchLabelExpressions): { matchLabelExpressions+: if std.isArray(v=matchLabelExpressions) then matchLabelExpressions else [matchLabelExpressions] },
  '#mixin': 'ignore',
  mixin: self
}