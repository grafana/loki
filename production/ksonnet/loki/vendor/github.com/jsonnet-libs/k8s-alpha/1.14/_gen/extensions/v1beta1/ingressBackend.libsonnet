{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='ingressBackend', url='', help='IngressBackend describes all endpoints for a given service and port.'),
  '#withServiceName':: d.fn(help='Specifies the name of the referenced service.', args=[d.arg(name='serviceName', type=d.T.string)]),
  withServiceName(serviceName): { serviceName: serviceName },
  '#withServicePort':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='servicePort', type=d.T.string)]),
  withServicePort(servicePort): { servicePort: servicePort },
  '#mixin': 'ignore',
  mixin: self
}