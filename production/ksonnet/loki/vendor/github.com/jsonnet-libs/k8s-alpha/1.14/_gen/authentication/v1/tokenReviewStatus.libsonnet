{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='tokenReviewStatus', url='', help='TokenReviewStatus is the result of the token authentication request.'),
  '#user':: d.obj(help='UserInfo holds the information about the user needed to implement the user.Info interface.'),
  user: {
    '#withExtra':: d.fn(help='Any additional information provided by the authenticator.', args=[d.arg(name='extra', type=d.T.object)]),
    withExtra(extra): { user+: { extra: extra } },
    '#withExtraMixin':: d.fn(help='Any additional information provided by the authenticator.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='extra', type=d.T.object)]),
    withExtraMixin(extra): { user+: { extra+: extra } },
    '#withGroups':: d.fn(help='The names of groups this user is a part of.', args=[d.arg(name='groups', type=d.T.array)]),
    withGroups(groups): { user+: { groups: if std.isArray(v=groups) then groups else [groups] } },
    '#withGroupsMixin':: d.fn(help='The names of groups this user is a part of.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='groups', type=d.T.array)]),
    withGroupsMixin(groups): { user+: { groups+: if std.isArray(v=groups) then groups else [groups] } },
    '#withUid':: d.fn(help='A unique value that identifies this user across time. If this user is deleted and another user by the same name is added, they will have different UIDs.', args=[d.arg(name='uid', type=d.T.string)]),
    withUid(uid): { user+: { uid: uid } },
    '#withUsername':: d.fn(help='The name that uniquely identifies this user among all active users.', args=[d.arg(name='username', type=d.T.string)]),
    withUsername(username): { user+: { username: username } }
  },
  '#withAudiences':: d.fn(help="Audiences are audience identifiers chosen by the authenticator that are compatible with both the TokenReview and token. An identifier is any identifier in the intersection of the TokenReviewSpec audiences and the token's audiences. A client of the TokenReview API that sets the spec.audiences field should validate that a compatible audience identifier is returned in the status.audiences field to ensure that the TokenReview server is audience aware. If a TokenReview returns an empty status.audience field where status.authenticated is 'true', the token is valid against the audience of the Kubernetes API server.", args=[d.arg(name='audiences', type=d.T.array)]),
  withAudiences(audiences): { audiences: if std.isArray(v=audiences) then audiences else [audiences] },
  '#withAudiencesMixin':: d.fn(help="Audiences are audience identifiers chosen by the authenticator that are compatible with both the TokenReview and token. An identifier is any identifier in the intersection of the TokenReviewSpec audiences and the token's audiences. A client of the TokenReview API that sets the spec.audiences field should validate that a compatible audience identifier is returned in the status.audiences field to ensure that the TokenReview server is audience aware. If a TokenReview returns an empty status.audience field where status.authenticated is 'true', the token is valid against the audience of the Kubernetes API server.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='audiences', type=d.T.array)]),
  withAudiencesMixin(audiences): { audiences+: if std.isArray(v=audiences) then audiences else [audiences] },
  '#withAuthenticated':: d.fn(help='Authenticated indicates that the token was associated with a known user.', args=[d.arg(name='authenticated', type=d.T.boolean)]),
  withAuthenticated(authenticated): { authenticated: authenticated },
  '#withError':: d.fn(help="Error indicates that the token couldn't be checked", args=[d.arg(name='err', type=d.T.string)]),
  withError(err): { 'error': err },
  '#mixin': 'ignore',
  mixin: self
}