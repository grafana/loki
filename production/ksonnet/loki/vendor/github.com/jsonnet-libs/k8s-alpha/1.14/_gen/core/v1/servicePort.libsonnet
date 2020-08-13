{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='servicePort', url='', help="ServicePort contains information on service's port."),
  '#withName':: d.fn(help="The name of this port within the service. This must be a DNS_LABEL. All ports within a ServiceSpec must have unique names. This maps to the 'Name' field in EndpointPort objects. Optional if only one ServicePort is defined on this service.", args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withNodePort':: d.fn(help='The port on each node on which this service is exposed when type=NodePort or LoadBalancer. Usually assigned by the system. If specified, it will be allocated to the service if unused or else creation of the service will fail. Default is to auto-allocate a port if the ServiceType of this Service requires one. More info: https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport', args=[d.arg(name='nodePort', type=d.T.integer)]),
  withNodePort(nodePort): { nodePort: nodePort },
  '#withPort':: d.fn(help='The port that will be exposed by this service.', args=[d.arg(name='port', type=d.T.integer)]),
  withPort(port): { port: port },
  '#withProtocol':: d.fn(help='The IP protocol for this port. Supports "TCP", "UDP", and "SCTP". Default is TCP.', args=[d.arg(name='protocol', type=d.T.string)]),
  withProtocol(protocol): { protocol: protocol },
  '#withTargetPort':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='targetPort', type=d.T.string)]),
  withTargetPort(targetPort): { targetPort: targetPort },
  '#mixin': 'ignore',
  mixin: self
}