{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='limitRangeSpec', url='', help='LimitRangeSpec defines a min/max usage limit for resources that match on kind.'),
  '#withLimits':: d.fn(help='Limits is the list of LimitRangeItem objects that are enforced.', args=[d.arg(name='limits', type=d.T.array)]),
  withLimits(limits): { limits: if std.isArray(v=limits) then limits else [limits] },
  '#withLimitsMixin':: d.fn(help='Limits is the list of LimitRangeItem objects that are enforced.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='limits', type=d.T.array)]),
  withLimitsMixin(limits): { limits+: if std.isArray(v=limits) then limits else [limits] },
  '#mixin': 'ignore',
  mixin: self
}