{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='rollingUpdateStatefulSetStrategy', url='', help='RollingUpdateStatefulSetStrategy is used to communicate parameter for RollingUpdateStatefulSetStrategyType.'),
  '#withPartition':: d.fn(help='Partition indicates the ordinal at which the StatefulSet should be partitioned.', args=[d.arg(name='partition', type=d.T.integer)]),
  withPartition(partition): { partition: partition },
  '#mixin': 'ignore',
  mixin: self
}