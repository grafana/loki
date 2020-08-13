{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeDaemonEndpoints', url='', help='NodeDaemonEndpoints lists ports opened by daemons running on the Node.'),
  '#kubeletEndpoint':: d.obj(help='DaemonEndpoint contains information about a single Daemon endpoint.'),
  kubeletEndpoint: {
    '#withPort':: d.fn(help='Port number of the given endpoint.', args=[d.arg(name='port', type=d.T.integer)]),
    withPort(port): { kubeletEndpoint+: { port: port } }
  },
  '#mixin': 'ignore',
  mixin: self
}