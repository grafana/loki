{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='ipBlock', url='', help="DEPRECATED 1.9 - This group version of IPBlock is deprecated by networking/v1/IPBlock. IPBlock describes a particular CIDR (Ex. '192.168.1.1/24') that is allowed to the pods matched by a NetworkPolicySpec's podSelector. The except entry describes CIDRs that should not be included within this rule."),
  '#withCidr':: d.fn(help='CIDR is a string representing the IP Block Valid examples are "192.168.1.1/24"', args=[d.arg(name='cidr', type=d.T.string)]),
  withCidr(cidr): { cidr: cidr },
  '#withExcept':: d.fn(help='Except is a slice of CIDRs that should not be included within an IP Block Valid examples are "192.168.1.1/24" Except values will be rejected if they are outside the CIDR range', args=[d.arg(name='except', type=d.T.array)]),
  withExcept(except): { except: if std.isArray(v=except) then except else [except] },
  '#withExceptMixin':: d.fn(help='Except is a slice of CIDRs that should not be included within an IP Block Valid examples are "192.168.1.1/24" Except values will be rejected if they are outside the CIDR range\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='except', type=d.T.array)]),
  withExceptMixin(except): { except+: if std.isArray(v=except) then except else [except] },
  '#mixin': 'ignore',
  mixin: self
}