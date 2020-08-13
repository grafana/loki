{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='seLinuxOptions', url='', help='SELinuxOptions are the labels to be applied to the container'),
  '#withLevel':: d.fn(help='Level is SELinux level label that applies to the container.', args=[d.arg(name='level', type=d.T.string)]),
  withLevel(level): { level: level },
  '#withRole':: d.fn(help='Role is a SELinux role label that applies to the container.', args=[d.arg(name='role', type=d.T.string)]),
  withRole(role): { role: role },
  '#withType':: d.fn(help='Type is a SELinux type label that applies to the container.', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#withUser':: d.fn(help='User is a SELinux user label that applies to the container.', args=[d.arg(name='user', type=d.T.string)]),
  withUser(user): { user: user },
  '#mixin': 'ignore',
  mixin: self
}