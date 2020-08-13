{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='scaleStatus', url='', help='represents the current status of a scale subresource.'),
  '#withReplicas':: d.fn(help='actual number of observed instances of the scaled object.', args=[d.arg(name='replicas', type=d.T.integer)]),
  withReplicas(replicas): { replicas: replicas },
  '#withSelector':: d.fn(help='label query over pods that should match the replicas count. More info: http://kubernetes.io/docs/user-guide/labels#label-selectors', args=[d.arg(name='selector', type=d.T.object)]),
  withSelector(selector): { selector: selector },
  '#withSelectorMixin':: d.fn(help='label query over pods that should match the replicas count. More info: http://kubernetes.io/docs/user-guide/labels#label-selectors\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='selector', type=d.T.object)]),
  withSelectorMixin(selector): { selector+: selector },
  '#withTargetSelector':: d.fn(help='label selector for pods that should match the replicas count. This is a serializated version of both map-based and more expressive set-based selectors. This is done to avoid introspection in the clients. The string will be in the same format as the query-param syntax. If the target type only supports map-based selectors, both this field and map-based selector field are populated. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors', args=[d.arg(name='targetSelector', type=d.T.string)]),
  withTargetSelector(targetSelector): { targetSelector: targetSelector },
  '#mixin': 'ignore',
  mixin: self
}