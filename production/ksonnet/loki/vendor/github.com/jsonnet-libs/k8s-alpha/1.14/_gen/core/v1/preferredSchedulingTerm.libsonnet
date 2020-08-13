{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='preferredSchedulingTerm', url='', help="An empty preferred scheduling term matches all objects with implicit weight 0 (i.e. it's a no-op). A null preferred scheduling term matches no objects (i.e. is also a no-op)."),
  '#preference':: d.obj(help='A null or empty node selector term matches no objects. The requirements of them are ANDed. The TopologySelectorTerm type implements a subset of the NodeSelectorTerm.'),
  preference: {
    '#withMatchExpressions':: d.fn(help="A list of node selector requirements by node's labels.", args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressions(matchExpressions): { preference+: { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchExpressionsMixin':: d.fn(help="A list of node selector requirements by node's labels.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressionsMixin(matchExpressions): { preference+: { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchFields':: d.fn(help="A list of node selector requirements by node's fields.", args=[d.arg(name='matchFields', type=d.T.array)]),
    withMatchFields(matchFields): { preference+: { matchFields: if std.isArray(v=matchFields) then matchFields else [matchFields] } },
    '#withMatchFieldsMixin':: d.fn(help="A list of node selector requirements by node's fields.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='matchFields', type=d.T.array)]),
    withMatchFieldsMixin(matchFields): { preference+: { matchFields+: if std.isArray(v=matchFields) then matchFields else [matchFields] } }
  },
  '#withWeight':: d.fn(help='Weight associated with matching the corresponding nodeSelectorTerm, in the range 1-100.', args=[d.arg(name='weight', type=d.T.integer)]),
  withWeight(weight): { weight: weight },
  '#mixin': 'ignore',
  mixin: self
}