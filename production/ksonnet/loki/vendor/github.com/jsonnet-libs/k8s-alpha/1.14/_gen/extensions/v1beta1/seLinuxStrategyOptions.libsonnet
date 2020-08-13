{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='seLinuxStrategyOptions', url='', help='SELinuxStrategyOptions defines the strategy type and any options used to create the strategy. Deprecated: use SELinuxStrategyOptions from policy API Group instead.'),
  '#seLinuxOptions':: d.obj(help='SELinuxOptions are the labels to be applied to the container'),
  seLinuxOptions: {
    '#withLevel':: d.fn(help='Level is SELinux level label that applies to the container.', args=[d.arg(name='level', type=d.T.string)]),
    withLevel(level): { seLinuxOptions+: { level: level } },
    '#withRole':: d.fn(help='Role is a SELinux role label that applies to the container.', args=[d.arg(name='role', type=d.T.string)]),
    withRole(role): { seLinuxOptions+: { role: role } },
    '#withType':: d.fn(help='Type is a SELinux type label that applies to the container.', args=[d.arg(name='type', type=d.T.string)]),
    withType(type): { seLinuxOptions+: { type: type } },
    '#withUser':: d.fn(help='User is a SELinux user label that applies to the container.', args=[d.arg(name='user', type=d.T.string)]),
    withUser(user): { seLinuxOptions+: { user: user } }
  },
  '#withRule':: d.fn(help='rule is the strategy that will dictate the allowable labels that may be set.', args=[d.arg(name='rule', type=d.T.string)]),
  withRule(rule): { rule: rule },
  '#mixin': 'ignore',
  mixin: self
}