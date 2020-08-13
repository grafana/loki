{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeSelectorTerm', url='', help='A null or empty node selector term matches no objects. The requirements of them are ANDed. The TopologySelectorTerm type implements a subset of the NodeSelectorTerm.'),
  '#withMatchExpressions':: d.fn(help="A list of node selector requirements by node's labels.", args=[d.arg(name='matchExpressions', type=d.T.array)]),
  withMatchExpressions(matchExpressions): { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] },
  '#withMatchExpressionsMixin':: d.fn(help="A list of node selector requirements by node's labels.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='matchExpressions', type=d.T.array)]),
  withMatchExpressionsMixin(matchExpressions): { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] },
  '#withMatchFields':: d.fn(help="A list of node selector requirements by node's fields.", args=[d.arg(name='matchFields', type=d.T.array)]),
  withMatchFields(matchFields): { matchFields: if std.isArray(v=matchFields) then matchFields else [matchFields] },
  '#withMatchFieldsMixin':: d.fn(help="A list of node selector requirements by node's fields.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='matchFields', type=d.T.array)]),
  withMatchFieldsMixin(matchFields): { matchFields+: if std.isArray(v=matchFields) then matchFields else [matchFields] },
  '#mixin': 'ignore',
  mixin: self
}