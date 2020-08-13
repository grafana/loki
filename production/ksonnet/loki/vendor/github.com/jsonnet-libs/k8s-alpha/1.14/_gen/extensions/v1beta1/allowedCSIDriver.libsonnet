{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='allowedCSIDriver', url='', help='AllowedCSIDriver represents a single inline CSI Driver that is allowed to be used.'),
  '#withName':: d.fn(help='Name is the registered name of the CSI driver', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#mixin': 'ignore',
  mixin: self
}