{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='httpIngressRuleValue', url='', help="HTTPIngressRuleValue is a list of http selectors pointing to backends. In the example: http://<host>/<path>?<searchpart> -> backend where where parts of the url correspond to RFC 3986, this resource will be used to match against everything after the last '/' and before the first '?' or '#'."),
  '#withPaths':: d.fn(help='A collection of paths that map requests to backends.', args=[d.arg(name='paths', type=d.T.array)]),
  withPaths(paths): { paths: if std.isArray(v=paths) then paths else [paths] },
  '#withPathsMixin':: d.fn(help='A collection of paths that map requests to backends.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='paths', type=d.T.array)]),
  withPathsMixin(paths): { paths+: if std.isArray(v=paths) then paths else [paths] },
  '#mixin': 'ignore',
  mixin: self
}