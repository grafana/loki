{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeAddress', url='', help="NodeAddress contains information for the node's address."),
  '#withAddress':: d.fn(help='The node address.', args=[d.arg(name='address', type=d.T.string)]),
  withAddress(address): { address: address },
  '#withType':: d.fn(help='Node address type, one of Hostname, ExternalIP or InternalIP.', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}