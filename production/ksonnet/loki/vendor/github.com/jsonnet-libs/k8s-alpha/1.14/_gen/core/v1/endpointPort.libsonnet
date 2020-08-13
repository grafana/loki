{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='endpointPort', url='', help='EndpointPort is a tuple that describes a single port.'),
  '#withName':: d.fn(help='The name of this port (corresponds to ServicePort.Name). Must be a DNS_LABEL. Optional only if one port is defined.', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withPort':: d.fn(help='The port number of the endpoint.', args=[d.arg(name='port', type=d.T.integer)]),
  withPort(port): { port: port },
  '#withProtocol':: d.fn(help='The IP protocol for this port. Must be UDP, TCP, or SCTP. Default is TCP.', args=[d.arg(name='protocol', type=d.T.string)]),
  withProtocol(protocol): { protocol: protocol },
  '#mixin': 'ignore',
  mixin: self
}