{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='taint', url='', help='The node this Taint is attached to has the "effect" on any pod that does not tolerate the Taint.'),
  '#withEffect':: d.fn(help='Required. The effect of the taint on pods that do not tolerate the taint. Valid effects are NoSchedule, PreferNoSchedule and NoExecute.', args=[d.arg(name='effect', type=d.T.string)]),
  withEffect(effect): { effect: effect },
  '#withKey':: d.fn(help='Required. The taint key to be applied to a node.', args=[d.arg(name='key', type=d.T.string)]),
  withKey(key): { key: key },
  '#withTimeAdded':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='timeAdded', type=d.T.string)]),
  withTimeAdded(timeAdded): { timeAdded: timeAdded },
  '#withValue':: d.fn(help='Required. The taint value corresponding to the taint key.', args=[d.arg(name='value', type=d.T.string)]),
  withValue(value): { value: value },
  '#mixin': 'ignore',
  mixin: self
}