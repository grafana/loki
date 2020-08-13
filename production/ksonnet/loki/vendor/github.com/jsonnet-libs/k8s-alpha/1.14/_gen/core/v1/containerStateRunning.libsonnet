{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='containerStateRunning', url='', help='ContainerStateRunning is a running state of a container.'),
  '#withStartedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='startedAt', type=d.T.string)]),
  withStartedAt(startedAt): { startedAt: startedAt },
  '#mixin': 'ignore',
  mixin: self
}