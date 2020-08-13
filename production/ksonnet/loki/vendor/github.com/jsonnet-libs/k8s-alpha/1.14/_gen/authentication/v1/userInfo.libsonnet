{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='userInfo', url='', help='UserInfo holds the information about the user needed to implement the user.Info interface.'),
  '#withExtra':: d.fn(help='Any additional information provided by the authenticator.', args=[d.arg(name='extra', type=d.T.object)]),
  withExtra(extra): { extra: extra },
  '#withExtraMixin':: d.fn(help='Any additional information provided by the authenticator.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='extra', type=d.T.object)]),
  withExtraMixin(extra): { extra+: extra },
  '#withGroups':: d.fn(help='The names of groups this user is a part of.', args=[d.arg(name='groups', type=d.T.array)]),
  withGroups(groups): { groups: if std.isArray(v=groups) then groups else [groups] },
  '#withGroupsMixin':: d.fn(help='The names of groups this user is a part of.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='groups', type=d.T.array)]),
  withGroupsMixin(groups): { groups+: if std.isArray(v=groups) then groups else [groups] },
  '#withUid':: d.fn(help='A unique value that identifies this user across time. If this user is deleted and another user by the same name is added, they will have different UIDs.', args=[d.arg(name='uid', type=d.T.string)]),
  withUid(uid): { uid: uid },
  '#withUsername':: d.fn(help='The name that uniquely identifies this user among all active users.', args=[d.arg(name='username', type=d.T.string)]),
  withUsername(username): { username: username },
  '#mixin': 'ignore',
  mixin: self
}