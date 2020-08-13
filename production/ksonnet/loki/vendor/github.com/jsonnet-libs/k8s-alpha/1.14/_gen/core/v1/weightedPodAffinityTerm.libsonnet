{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='weightedPodAffinityTerm', url='', help='The weights of all of the matched WeightedPodAffinityTerm fields are added per-node to find the most preferred node(s)'),
  '#podAffinityTerm':: d.obj(help='Defines a set of pods (namely those matching the labelSelector relative to the given namespace(s)) that this pod should be co-located (affinity) or not co-located (anti-affinity) with, where co-located is defined as running on a node whose value of the label with key <topologyKey> matches that of any node on which a pod of the set of pods is running'),
  podAffinityTerm: {
    '#labelSelector':: d.obj(help='A label selector is a label query over a set of resources. The result of matchLabels and matchExpressions are ANDed. An empty label selector matches all objects. A null label selector matches no objects.'),
    labelSelector: {
      '#withMatchExpressions':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.', args=[d.arg(name='matchExpressions', type=d.T.array)]),
      withMatchExpressions(matchExpressions): { podAffinityTerm+: { labelSelector+: { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } } },
      '#withMatchExpressionsMixin':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchExpressions', type=d.T.array)]),
      withMatchExpressionsMixin(matchExpressions): { podAffinityTerm+: { labelSelector+: { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } } },
      '#withMatchLabels':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.', args=[d.arg(name='matchLabels', type=d.T.object)]),
      withMatchLabels(matchLabels): { podAffinityTerm+: { labelSelector+: { matchLabels: matchLabels } } },
      '#withMatchLabelsMixin':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchLabels', type=d.T.object)]),
      withMatchLabelsMixin(matchLabels): { podAffinityTerm+: { labelSelector+: { matchLabels+: matchLabels } } }
    },
    '#withNamespaces':: d.fn(help="namespaces specifies which namespaces the labelSelector applies to (matches against); null or empty list means 'this pod's namespace'", args=[d.arg(name='namespaces', type=d.T.array)]),
    withNamespaces(namespaces): { podAffinityTerm+: { namespaces: if std.isArray(v=namespaces) then namespaces else [namespaces] } },
    '#withNamespacesMixin':: d.fn(help="namespaces specifies which namespaces the labelSelector applies to (matches against); null or empty list means 'this pod's namespace'\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='namespaces', type=d.T.array)]),
    withNamespacesMixin(namespaces): { podAffinityTerm+: { namespaces+: if std.isArray(v=namespaces) then namespaces else [namespaces] } },
    '#withTopologyKey':: d.fn(help='This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching the labelSelector in the specified namespaces, where co-located is defined as running on a node whose value of the label with key topologyKey matches that of any node on which any of the selected pods is running. Empty topologyKey is not allowed.', args=[d.arg(name='topologyKey', type=d.T.string)]),
    withTopologyKey(topologyKey): { podAffinityTerm+: { topologyKey: topologyKey } }
  },
  '#withWeight':: d.fn(help='weight associated with matching the corresponding podAffinityTerm, in the range 1-100.', args=[d.arg(name='weight', type=d.T.integer)]),
  withWeight(weight): { weight: weight },
  '#mixin': 'ignore',
  mixin: self
}