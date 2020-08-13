{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='statefulSetUpdateStrategy', url='', help='StatefulSetUpdateStrategy indicates the strategy that the StatefulSet controller will use to perform updates. It includes any additional parameters necessary to perform the update for the indicated strategy.'),
  '#rollingUpdate':: d.obj(help='RollingUpdateStatefulSetStrategy is used to communicate parameter for RollingUpdateStatefulSetStrategyType.'),
  rollingUpdate: {
    '#withPartition':: d.fn(help='Partition indicates the ordinal at which the StatefulSet should be partitioned.', args=[d.arg(name='partition', type=d.T.integer)]),
    withPartition(partition): { rollingUpdate+: { partition: partition } }
  },
  '#withType':: d.fn(help='Type indicates the type of the StatefulSetUpdateStrategy.', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}