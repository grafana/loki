{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='componentCondition', url='', help='Information about the condition of a component.'),
  '#withError':: d.fn(help='Condition error code for a component. For example, a health check error code.', args=[d.arg(name='err', type=d.T.string)]),
  withError(err): { 'error': err },
  '#withMessage':: d.fn(help='Message about the condition for a component. For example, information about a health check.', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withType':: d.fn(help='Type of condition for a component. Valid value: "Healthy"', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}