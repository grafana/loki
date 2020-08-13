{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='hostPortRange', url='', help='HostPortRange defines a range of host ports that will be enabled by a policy for pods to use.  It requires both the start and end to be defined. Deprecated: use HostPortRange from policy API Group instead.'),
  '#withMax':: d.fn(help='max is the end of the range, inclusive.', args=[d.arg(name='max', type=d.T.integer)]),
  withMax(max): { max: max },
  '#withMin':: d.fn(help='min is the start of the range, inclusive.', args=[d.arg(name='min', type=d.T.integer)]),
  withMin(min): { min: min },
  '#mixin': 'ignore',
  mixin: self
}