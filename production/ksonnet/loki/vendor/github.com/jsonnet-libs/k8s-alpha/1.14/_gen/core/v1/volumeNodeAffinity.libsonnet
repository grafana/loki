{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeNodeAffinity', url='', help='VolumeNodeAffinity defines constraints that limit what nodes this volume can be accessed from.'),
  '#required':: d.obj(help='A node selector represents the union of the results of one or more label queries over a set of nodes; that is, it represents the OR of the selectors represented by the node selector terms.'),
  required: {
    '#withNodeSelectorTerms':: d.fn(help='Required. A list of node selector terms. The terms are ORed.', args=[d.arg(name='nodeSelectorTerms', type=d.T.array)]),
    withNodeSelectorTerms(nodeSelectorTerms): { required+: { nodeSelectorTerms: if std.isArray(v=nodeSelectorTerms) then nodeSelectorTerms else [nodeSelectorTerms] } },
    '#withNodeSelectorTermsMixin':: d.fn(help='Required. A list of node selector terms. The terms are ORed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='nodeSelectorTerms', type=d.T.array)]),
    withNodeSelectorTermsMixin(nodeSelectorTerms): { required+: { nodeSelectorTerms+: if std.isArray(v=nodeSelectorTerms) then nodeSelectorTerms else [nodeSelectorTerms] } }
  },
  '#mixin': 'ignore',
  mixin: self
}