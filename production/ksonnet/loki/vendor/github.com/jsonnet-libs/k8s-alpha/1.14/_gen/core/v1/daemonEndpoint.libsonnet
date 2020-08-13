{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='daemonEndpoint', url='', help='DaemonEndpoint contains information about a single Daemon endpoint.'),
  '#withPort':: d.fn(help='Port number of the given endpoint.', args=[d.arg(name='port', type=d.T.integer)]),
  withPort(port): { port: port },
  '#mixin': 'ignore',
  mixin: self
}