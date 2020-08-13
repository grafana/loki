{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='httpIngressPath', url='', help='HTTPIngressPath associates a path regex with a backend. Incoming urls matching the path are forwarded to the backend.'),
  '#backend':: d.obj(help='IngressBackend describes all endpoints for a given service and port.'),
  backend: {
    '#withServiceName':: d.fn(help='Specifies the name of the referenced service.', args=[d.arg(name='serviceName', type=d.T.string)]),
    withServiceName(serviceName): { backend+: { serviceName: serviceName } },
    '#withServicePort':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='servicePort', type=d.T.string)]),
    withServicePort(servicePort): { backend+: { servicePort: servicePort } }
  },
  '#withPath':: d.fn(help="Path is an extended POSIX regex as defined by IEEE Std 1003.1, (i.e this follows the egrep/unix syntax, not the perl syntax) matched against the path of an incoming request. Currently it can contain characters disallowed from the conventional 'path' part of a URL as defined by RFC 3986. Paths must begin with a '/'. If unspecified, the path defaults to a catch all sending traffic to the backend.", args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#mixin': 'ignore',
  mixin: self
}