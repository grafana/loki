{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeError', url='', help='VolumeError captures an error encountered during a volume operation.'),
  '#withMessage':: d.fn(help='String detailing the error encountered during Attach or Detach operation. This string maybe logged, so it should not contain sensitive information.', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='time', type=d.T.string)]),
  withTime(time): { time: time },
  '#mixin': 'ignore',
  mixin: self
}