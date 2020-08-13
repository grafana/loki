{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='networkPolicyPeer', url='', help='NetworkPolicyPeer describes a peer to allow traffic from. Only certain combinations of fields are allowed'),
  '#ipBlock':: d.obj(help="IPBlock describes a particular CIDR (Ex. '192.168.1.1/24') that is allowed to the pods matched by a NetworkPolicySpec's podSelector. The except entry describes CIDRs that should not be included within this rule."),
  ipBlock: {
    '#withCidr':: d.fn(help='CIDR is a string representing the IP Block Valid examples are "192.168.1.1/24"', args=[d.arg(name='cidr', type=d.T.string)]),
    withCidr(cidr): { ipBlock+: { cidr: cidr } },
    '#withExcept':: d.fn(help='Except is a slice of CIDRs that should not be included within an IP Block Valid examples are "192.168.1.1/24" Except values will be rejected if they are outside the CIDR range', args=[d.arg(name='except', type=d.T.array)]),
    withExcept(except): { ipBlock+: { except: if std.isArray(v=except) then except else [except] } },
    '#withExceptMixin':: d.fn(help='Except is a slice of CIDRs that should not be included within an IP Block Valid examples are "192.168.1.1/24" Except values will be rejected if they are outside the CIDR range\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='except', type=d.T.array)]),
    withExceptMixin(except): { ipBlock+: { except+: if std.isArray(v=except) then except else [except] } }
  },
  '#namespaceSelector':: d.obj(help='A label selector is a label query over a set of resources. The result of matchLabels and matchExpressions are ANDed. An empty label selector matches all objects. A null label selector matches no objects.'),
  namespaceSelector: {
    '#withMatchExpressions':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressions(matchExpressions): { namespaceSelector+: { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchExpressionsMixin':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressionsMixin(matchExpressions): { namespaceSelector+: { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchLabels':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabels(matchLabels): { namespaceSelector+: { matchLabels: matchLabels } },
    '#withMatchLabelsMixin':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabelsMixin(matchLabels): { namespaceSelector+: { matchLabels+: matchLabels } }
  },
  '#podSelector':: d.obj(help='A label selector is a label query over a set of resources. The result of matchLabels and matchExpressions are ANDed. An empty label selector matches all objects. A null label selector matches no objects.'),
  podSelector: {
    '#withMatchExpressions':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressions(matchExpressions): { podSelector+: { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchExpressionsMixin':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressionsMixin(matchExpressions): { podSelector+: { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchLabels':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabels(matchLabels): { podSelector+: { matchLabels: matchLabels } },
    '#withMatchLabelsMixin':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabelsMixin(matchLabels): { podSelector+: { matchLabels+: matchLabels } }
  },
  '#mixin': 'ignore',
  mixin: self
}