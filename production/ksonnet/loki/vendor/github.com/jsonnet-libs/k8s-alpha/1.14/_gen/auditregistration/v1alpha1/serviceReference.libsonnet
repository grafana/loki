{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='serviceReference', url='', help='ServiceReference holds a reference to Service.legacy.k8s.io'),
  '#withName':: d.fn(help='`name` is the name of the service. Required', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withNamespace':: d.fn(help='`namespace` is the namespace of the service. Required', args=[d.arg(name='namespace', type=d.T.string)]),
  withNamespace(namespace): { namespace: namespace },
  '#withPath':: d.fn(help='`path` is an optional URL path which will be sent in any request to this service.', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#mixin': 'ignore',
  mixin: self
}