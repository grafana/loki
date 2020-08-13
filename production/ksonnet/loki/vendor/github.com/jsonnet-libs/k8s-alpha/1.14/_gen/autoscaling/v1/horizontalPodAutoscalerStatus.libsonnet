{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='horizontalPodAutoscalerStatus', url='', help='current status of a horizontal pod autoscaler'),
  '#withCurrentCPUUtilizationPercentage':: d.fn(help='current average CPU utilization over all pods, represented as a percentage of requested CPU, e.g. 70 means that an average pod is using now 70% of its requested CPU.', args=[d.arg(name='currentCPUUtilizationPercentage', type=d.T.integer)]),
  withCurrentCPUUtilizationPercentage(currentCPUUtilizationPercentage): { currentCPUUtilizationPercentage: currentCPUUtilizationPercentage },
  '#withCurrentReplicas':: d.fn(help='current number of replicas of pods managed by this autoscaler.', args=[d.arg(name='currentReplicas', type=d.T.integer)]),
  withCurrentReplicas(currentReplicas): { currentReplicas: currentReplicas },
  '#withDesiredReplicas':: d.fn(help='desired number of replicas of pods managed by this autoscaler.', args=[d.arg(name='desiredReplicas', type=d.T.integer)]),
  withDesiredReplicas(desiredReplicas): { desiredReplicas: desiredReplicas },
  '#withLastScaleTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastScaleTime', type=d.T.string)]),
  withLastScaleTime(lastScaleTime): { lastScaleTime: lastScaleTime },
  '#withObservedGeneration':: d.fn(help='most recent generation observed by this autoscaler.', args=[d.arg(name='observedGeneration', type=d.T.integer)]),
  withObservedGeneration(observedGeneration): { observedGeneration: observedGeneration },
  '#mixin': 'ignore',
  mixin: self
}