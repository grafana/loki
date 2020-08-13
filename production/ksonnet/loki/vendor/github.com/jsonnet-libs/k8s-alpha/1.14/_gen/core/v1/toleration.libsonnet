{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='toleration', url='', help='The pod this Toleration is attached to tolerates any taint that matches the triple <key,value,effect> using the matching operator <operator>.'),
  '#withEffect':: d.fn(help='Effect indicates the taint effect to match. Empty means match all taint effects. When specified, allowed values are NoSchedule, PreferNoSchedule and NoExecute.', args=[d.arg(name='effect', type=d.T.string)]),
  withEffect(effect): { effect: effect },
  '#withKey':: d.fn(help='Key is the taint key that the toleration applies to. Empty means match all taint keys. If the key is empty, operator must be Exists; this combination means to match all values and all keys.', args=[d.arg(name='key', type=d.T.string)]),
  withKey(key): { key: key },
  '#withOperator':: d.fn(help="Operator represents a key's relationship to the value. Valid operators are Exists and Equal. Defaults to Equal. Exists is equivalent to wildcard for value, so that a pod can tolerate all taints of a particular category.", args=[d.arg(name='operator', type=d.T.string)]),
  withOperator(operator): { operator: operator },
  '#withTolerationSeconds':: d.fn(help='TolerationSeconds represents the period of time the toleration (which must be of effect NoExecute, otherwise this field is ignored) tolerates the taint. By default, it is not set, which means tolerate the taint forever (do not evict). Zero and negative values will be treated as 0 (evict immediately) by the system.', args=[d.arg(name='tolerationSeconds', type=d.T.integer)]),
  withTolerationSeconds(tolerationSeconds): { tolerationSeconds: tolerationSeconds },
  '#withValue':: d.fn(help='Value is the taint value the toleration matches to. If the operator is Exists, the value should be empty, otherwise just a regular string.', args=[d.arg(name='value', type=d.T.string)]),
  withValue(value): { value: value },
  '#mixin': 'ignore',
  mixin: self
}