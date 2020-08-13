{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeCondition', url='', help='NodeCondition contains condition information for a node.'),
  '#withLastHeartbeatTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastHeartbeatTime', type=d.T.string)]),
  withLastHeartbeatTime(lastHeartbeatTime): { lastHeartbeatTime: lastHeartbeatTime },
  '#withLastTransitionTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastTransitionTime', type=d.T.string)]),
  withLastTransitionTime(lastTransitionTime): { lastTransitionTime: lastTransitionTime },
  '#withMessage':: d.fn(help='Human readable message indicating details about last transition.', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withReason':: d.fn(help="(brief) reason for the condition's last transition.", args=[d.arg(name='reason', type=d.T.string)]),
  withReason(reason): { reason: reason },
  '#withType':: d.fn(help='Type of node condition.', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}