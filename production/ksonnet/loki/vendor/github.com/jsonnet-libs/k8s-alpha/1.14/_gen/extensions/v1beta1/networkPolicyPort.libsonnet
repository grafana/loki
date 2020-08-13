{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='networkPolicyPort', url='', help='DEPRECATED 1.9 - This group version of NetworkPolicyPort is deprecated by networking/v1/NetworkPolicyPort.'),
  '#withPort':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='port', type=d.T.string)]),
  withPort(port): { port: port },
  '#withProtocol':: d.fn(help='Optional.  The protocol (TCP, UDP, or SCTP) which traffic must match. If not specified, this field defaults to TCP.', args=[d.arg(name='protocol', type=d.T.string)]),
  withProtocol(protocol): { protocol: protocol },
  '#mixin': 'ignore',
  mixin: self
}