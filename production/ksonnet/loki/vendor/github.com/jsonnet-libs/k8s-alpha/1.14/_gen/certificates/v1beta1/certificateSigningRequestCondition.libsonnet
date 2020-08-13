{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='certificateSigningRequestCondition', url='', help=''),
  '#withLastUpdateTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='lastUpdateTime', type=d.T.string)]),
  withLastUpdateTime(lastUpdateTime): { lastUpdateTime: lastUpdateTime },
  '#withMessage':: d.fn(help='human readable message with details about the request state', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withReason':: d.fn(help='brief reason for the request state', args=[d.arg(name='reason', type=d.T.string)]),
  withReason(reason): { reason: reason },
  '#withType':: d.fn(help='request approval state, currently Approved or Denied.', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}