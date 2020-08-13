{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='networkPolicyEgressRule', url='', help="NetworkPolicyEgressRule describes a particular set of traffic that is allowed out of pods matched by a NetworkPolicySpec's podSelector. The traffic must match both ports and to. This type is beta-level in 1.8"),
  '#withPorts':: d.fn(help='List of destination ports for outgoing traffic. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.', args=[d.arg(name='ports', type=d.T.array)]),
  withPorts(ports): { ports: if std.isArray(v=ports) then ports else [ports] },
  '#withPortsMixin':: d.fn(help='List of destination ports for outgoing traffic. Each item in this list is combined using a logical OR. If this field is empty or missing, this rule matches all ports (traffic not restricted by port). If this field is present and contains at least one item, then this rule allows traffic only if the traffic matches at least one port in the list.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='ports', type=d.T.array)]),
  withPortsMixin(ports): { ports+: if std.isArray(v=ports) then ports else [ports] },
  '#withTo':: d.fn(help='List of destinations for outgoing traffic of pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all destinations (traffic not restricted by destination). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the to list.', args=[d.arg(name='to', type=d.T.array)]),
  withTo(to): { to: if std.isArray(v=to) then to else [to] },
  '#withToMixin':: d.fn(help='List of destinations for outgoing traffic of pods selected for this rule. Items in this list are combined using a logical OR operation. If this field is empty or missing, this rule matches all destinations (traffic not restricted by destination). If this field is present and contains at least one item, this rule allows traffic only if the traffic matches at least one item in the to list.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='to', type=d.T.array)]),
  withToMixin(to): { to+: if std.isArray(v=to) then to else [to] },
  '#mixin': 'ignore',
  mixin: self
}