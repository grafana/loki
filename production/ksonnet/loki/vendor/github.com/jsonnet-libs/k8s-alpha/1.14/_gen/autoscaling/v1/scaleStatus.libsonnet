{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='scaleStatus', url='', help='ScaleStatus represents the current status of a scale subresource.'),
  '#withReplicas':: d.fn(help='actual number of observed instances of the scaled object.', args=[d.arg(name='replicas', type=d.T.integer)]),
  withReplicas(replicas): { replicas: replicas },
  '#withSelector':: d.fn(help='label query over pods that should match the replicas count. This is same as the label selector but in the string format to avoid introspection by clients. The string will be in the same format as the query-param syntax. More info about label selectors: http://kubernetes.io/docs/user-guide/labels#label-selectors', args=[d.arg(name='selector', type=d.T.string)]),
  withSelector(selector): { selector: selector },
  '#mixin': 'ignore',
  mixin: self
}