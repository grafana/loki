{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='podReadinessGate', url='', help='PodReadinessGate contains the reference to a pod condition'),
  '#withConditionType':: d.fn(help="ConditionType refers to a condition in the pod's condition list with matching type.", args=[d.arg(name='conditionType', type=d.T.string)]),
  withConditionType(conditionType): { conditionType: conditionType },
  '#mixin': 'ignore',
  mixin: self
}