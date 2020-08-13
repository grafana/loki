{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='idRange', url='', help='IDRange provides a min/max of an allowed range of IDs. Deprecated: use IDRange from policy API Group instead.'),
  '#withMax':: d.fn(help='max is the end of the range, inclusive.', args=[d.arg(name='max', type=d.T.integer)]),
  withMax(max): { max: max },
  '#withMin':: d.fn(help='min is the start of the range, inclusive.', args=[d.arg(name='min', type=d.T.integer)]),
  withMin(min): { min: min },
  '#mixin': 'ignore',
  mixin: self
}