{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='eventSource', url='', help='EventSource contains information for an event.'),
  '#withComponent':: d.fn(help='Component from which the event is generated.', args=[d.arg(name='component', type=d.T.string)]),
  withComponent(component): { component: component },
  '#withHost':: d.fn(help='Node name on which the event is generated.', args=[d.arg(name='host', type=d.T.string)]),
  withHost(host): { host: host },
  '#mixin': 'ignore',
  mixin: self
}