{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='containerPort', url='', help='ContainerPort represents a network port in a single container.'),
  '#withContainerPort':: d.fn(help="Number of port to expose on the pod's IP address. This must be a valid port number, 0 < x < 65536.", args=[d.arg(name='containerPort', type=d.T.integer)]),
  withContainerPort(containerPort): { containerPort: containerPort },
  '#withHostIP':: d.fn(help='What host IP to bind the external port to.', args=[d.arg(name='hostIP', type=d.T.string)]),
  withHostIP(hostIP): { hostIP: hostIP },
  '#withHostPort':: d.fn(help='Number of port to expose on the host. If specified, this must be a valid port number, 0 < x < 65536. If HostNetwork is specified, this must match ContainerPort. Most containers do not need this.', args=[d.arg(name='hostPort', type=d.T.integer)]),
  withHostPort(hostPort): { hostPort: hostPort },
  '#withName':: d.fn(help='If specified, this must be an IANA_SVC_NAME and unique within the pod. Each named port in a pod must have a unique name. Name for the port that can be referred to by services.', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withProtocol':: d.fn(help='Protocol for port. Must be UDP, TCP, or SCTP. Defaults to "TCP".', args=[d.arg(name='protocol', type=d.T.string)]),
  withProtocol(protocol): { protocol: protocol },
  '#mixin': 'ignore',
  mixin: self
}