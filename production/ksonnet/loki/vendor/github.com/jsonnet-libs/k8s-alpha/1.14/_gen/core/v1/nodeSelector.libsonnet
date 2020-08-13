{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeSelector', url='', help='A node selector represents the union of the results of one or more label queries over a set of nodes; that is, it represents the OR of the selectors represented by the node selector terms.'),
  '#withNodeSelectorTerms':: d.fn(help='Required. A list of node selector terms. The terms are ORed.', args=[d.arg(name='nodeSelectorTerms', type=d.T.array)]),
  withNodeSelectorTerms(nodeSelectorTerms): { nodeSelectorTerms: if std.isArray(v=nodeSelectorTerms) then nodeSelectorTerms else [nodeSelectorTerms] },
  '#withNodeSelectorTermsMixin':: d.fn(help='Required. A list of node selector terms. The terms are ORed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='nodeSelectorTerms', type=d.T.array)]),
  withNodeSelectorTermsMixin(nodeSelectorTerms): { nodeSelectorTerms+: if std.isArray(v=nodeSelectorTerms) then nodeSelectorTerms else [nodeSelectorTerms] },
  '#mixin': 'ignore',
  mixin: self
}