{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nonResourceAttributes', url='', help='NonResourceAttributes includes the authorization attributes available for non-resource requests to the Authorizer interface'),
  '#withPath':: d.fn(help='Path is the URL path of the request', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#withVerb':: d.fn(help='Verb is the standard HTTP verb', args=[d.arg(name='verb', type=d.T.string)]),
  withVerb(verb): { verb: verb },
  '#mixin': 'ignore',
  mixin: self
}