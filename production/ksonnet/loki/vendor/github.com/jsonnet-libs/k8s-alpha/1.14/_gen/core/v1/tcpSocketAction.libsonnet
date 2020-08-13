{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='tcpSocketAction', url='', help='TCPSocketAction describes an action based on opening a socket'),
  '#withHost':: d.fn(help='Optional: Host name to connect to, defaults to the pod IP.', args=[d.arg(name='host', type=d.T.string)]),
  withHost(host): { host: host },
  '#withPort':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='port', type=d.T.string)]),
  withPort(port): { port: port },
  '#mixin': 'ignore',
  mixin: self
}