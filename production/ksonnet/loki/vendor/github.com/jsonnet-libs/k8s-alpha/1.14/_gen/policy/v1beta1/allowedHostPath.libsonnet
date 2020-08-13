{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='allowedHostPath', url='', help='AllowedHostPath defines the host volume conditions that will be enabled by a policy for pods to use. It requires the path prefix to be defined.'),
  '#withPathPrefix':: d.fn(help='pathPrefix is the path prefix that the host volume must match. It does not support `*`. Trailing slashes are trimmed when validating the path prefix with a host path.\n\nExamples: `/foo` would allow `/foo`, `/foo/` and `/foo/bar` `/foo` would not allow `/food` or `/etc/foo`', args=[d.arg(name='pathPrefix', type=d.T.string)]),
  withPathPrefix(pathPrefix): { pathPrefix: pathPrefix },
  '#withReadOnly':: d.fn(help='when set to true, will allow host volumes matching the pathPrefix only if all volume mounts are readOnly.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#mixin': 'ignore',
  mixin: self
}