{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='httpGetAction', url='', help='HTTPGetAction describes an action based on HTTP Get requests.'),
  '#withHost':: d.fn(help='Host name to connect to, defaults to the pod IP. You probably want to set "Host" in httpHeaders instead.', args=[d.arg(name='host', type=d.T.string)]),
  withHost(host): { host: host },
  '#withHttpHeaders':: d.fn(help='Custom headers to set in the request. HTTP allows repeated headers.', args=[d.arg(name='httpHeaders', type=d.T.array)]),
  withHttpHeaders(httpHeaders): { httpHeaders: if std.isArray(v=httpHeaders) then httpHeaders else [httpHeaders] },
  '#withHttpHeadersMixin':: d.fn(help='Custom headers to set in the request. HTTP allows repeated headers.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='httpHeaders', type=d.T.array)]),
  withHttpHeadersMixin(httpHeaders): { httpHeaders+: if std.isArray(v=httpHeaders) then httpHeaders else [httpHeaders] },
  '#withPath':: d.fn(help='Path to access on the HTTP server.', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#withPort':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='port', type=d.T.string)]),
  withPort(port): { port: port },
  '#withScheme':: d.fn(help='Scheme to use for connecting to the host. Defaults to HTTP.', args=[d.arg(name='scheme', type=d.T.string)]),
  withScheme(scheme): { scheme: scheme },
  '#mixin': 'ignore',
  mixin: self
}