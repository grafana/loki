{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='horizontalPodAutoscalerSpec', url='', help='HorizontalPodAutoscalerSpec describes the desired functionality of the HorizontalPodAutoscaler.'),
  '#scaleTargetRef':: d.obj(help='CrossVersionObjectReference contains enough information to let you identify the referred resource.'),
  scaleTargetRef: {
    '#withKind':: d.fn(help='Kind of the referent; More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds"', args=[d.arg(name='kind', type=d.T.string)]),
    withKind(kind): { scaleTargetRef+: { kind: kind } },
    '#withName':: d.fn(help='Name of the referent; More info: http://kubernetes.io/docs/user-guide/identifiers#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { scaleTargetRef+: { name: name } }
  },
  '#withMaxReplicas':: d.fn(help='maxReplicas is the upper limit for the number of replicas to which the autoscaler can scale up. It cannot be less that minReplicas.', args=[d.arg(name='maxReplicas', type=d.T.integer)]),
  withMaxReplicas(maxReplicas): { maxReplicas: maxReplicas },
  '#withMetrics':: d.fn(help='metrics contains the specifications for which to use to calculate the desired replica count (the maximum replica count across all metrics will be used).  The desired replica count is calculated multiplying the ratio between the target value and the current value by the current number of pods.  Ergo, metrics used must decrease as the pod count is increased, and vice-versa.  See the individual metric source types for more information about how each type of metric must respond.', args=[d.arg(name='metrics', type=d.T.array)]),
  withMetrics(metrics): { metrics: if std.isArray(v=metrics) then metrics else [metrics] },
  '#withMetricsMixin':: d.fn(help='metrics contains the specifications for which to use to calculate the desired replica count (the maximum replica count across all metrics will be used).  The desired replica count is calculated multiplying the ratio between the target value and the current value by the current number of pods.  Ergo, metrics used must decrease as the pod count is increased, and vice-versa.  See the individual metric source types for more information about how each type of metric must respond.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='metrics', type=d.T.array)]),
  withMetricsMixin(metrics): { metrics+: if std.isArray(v=metrics) then metrics else [metrics] },
  '#withMinReplicas':: d.fn(help='minReplicas is the lower limit for the number of replicas to which the autoscaler can scale down. It defaults to 1 pod.', args=[d.arg(name='minReplicas', type=d.T.integer)]),
  withMinReplicas(minReplicas): { minReplicas: minReplicas },
  '#mixin': 'ignore',
  mixin: self
}