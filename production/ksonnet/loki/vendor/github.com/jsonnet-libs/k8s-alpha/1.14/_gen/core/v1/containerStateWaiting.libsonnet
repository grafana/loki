{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='containerStateWaiting', url='', help='ContainerStateWaiting is a waiting state of a container.'),
  '#withMessage':: d.fn(help='Message regarding why the container is not yet running.', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withReason':: d.fn(help='(brief) reason the container is not yet running.', args=[d.arg(name='reason', type=d.T.string)]),
  withReason(reason): { reason: reason },
  '#mixin': 'ignore',
  mixin: self
}