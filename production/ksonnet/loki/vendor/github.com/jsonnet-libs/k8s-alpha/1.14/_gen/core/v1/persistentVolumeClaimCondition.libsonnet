{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='persistentVolumeClaimCondition', url='', help='PersistentVolumeClaimCondition contails details about state of pvc'),
  '#withLastProbeTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastProbeTime', type=d.T.string)]),
  withLastProbeTime(lastProbeTime): { lastProbeTime: lastProbeTime },
  '#withLastTransitionTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastTransitionTime', type=d.T.string)]),
  withLastTransitionTime(lastTransitionTime): { lastTransitionTime: lastTransitionTime },
  '#withMessage':: d.fn(help='Human-readable message indicating details about last transition.', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withReason':: d.fn(help="Unique, this should be a short, machine understandable string that gives the reason for condition's last transition. If it reports 'ResizeStarted' that means the underlying persistent volume is being resized.", args=[d.arg(name='reason', type=d.T.string)]),
  withReason(reason): { reason: reason },
  '#withType':: d.fn(help='', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}