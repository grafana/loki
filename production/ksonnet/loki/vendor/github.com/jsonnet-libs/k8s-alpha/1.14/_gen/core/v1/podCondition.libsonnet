{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='podCondition', url='', help='PodCondition contains details for the current condition of this pod.'),
  '#withLastProbeTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastProbeTime', type=d.T.string)]),
  withLastProbeTime(lastProbeTime): { lastProbeTime: lastProbeTime },
  '#withLastTransitionTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastTransitionTime', type=d.T.string)]),
  withLastTransitionTime(lastTransitionTime): { lastTransitionTime: lastTransitionTime },
  '#withMessage':: d.fn(help='Human-readable message indicating details about last transition.', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withReason':: d.fn(help="Unique, one-word, CamelCase reason for the condition's last transition.", args=[d.arg(name='reason', type=d.T.string)]),
  withReason(reason): { reason: reason },
  '#withType':: d.fn(help='Type is the type of the condition. More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-conditions', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}