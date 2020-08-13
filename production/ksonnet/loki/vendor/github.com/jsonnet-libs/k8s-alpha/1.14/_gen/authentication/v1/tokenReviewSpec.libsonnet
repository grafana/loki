{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='tokenReviewSpec', url='', help='TokenReviewSpec is a description of the token authentication request.'),
  '#withAudiences':: d.fn(help='Audiences is a list of the identifiers that the resource server presented with the token identifies as. Audience-aware token authenticators will verify that the token was intended for at least one of the audiences in this list. If no audiences are provided, the audience will default to the audience of the Kubernetes apiserver.', args=[d.arg(name='audiences', type=d.T.array)]),
  withAudiences(audiences): { audiences: if std.isArray(v=audiences) then audiences else [audiences] },
  '#withAudiencesMixin':: d.fn(help='Audiences is a list of the identifiers that the resource server presented with the token identifies as. Audience-aware token authenticators will verify that the token was intended for at least one of the audiences in this list. If no audiences are provided, the audience will default to the audience of the Kubernetes apiserver.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='audiences', type=d.T.array)]),
  withAudiencesMixin(audiences): { audiences+: if std.isArray(v=audiences) then audiences else [audiences] },
  '#withToken':: d.fn(help='Token is the opaque bearer token.', args=[d.arg(name='token', type=d.T.string)]),
  withToken(token): { token: token },
  '#mixin': 'ignore',
  mixin: self
}