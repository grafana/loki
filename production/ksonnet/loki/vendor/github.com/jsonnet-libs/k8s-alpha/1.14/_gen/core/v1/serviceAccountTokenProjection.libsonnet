{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='serviceAccountTokenProjection', url='', help='ServiceAccountTokenProjection represents a projected service account token volume. This projection can be used to insert a service account token into the pods runtime filesystem for use against APIs (Kubernetes API Server or otherwise).'),
  '#withAudience':: d.fn(help='Audience is the intended audience of the token. A recipient of a token must identify itself with an identifier specified in the audience of the token, and otherwise should reject the token. The audience defaults to the identifier of the apiserver.', args=[d.arg(name='audience', type=d.T.string)]),
  withAudience(audience): { audience: audience },
  '#withExpirationSeconds':: d.fn(help='ExpirationSeconds is the requested duration of validity of the service account token. As the token approaches expiration, the kubelet volume plugin will proactively rotate the service account token. The kubelet will start trying to rotate the token if the token is older than 80 percent of its time to live or if the token is older than 24 hours.Defaults to 1 hour and must be at least 10 minutes.', args=[d.arg(name='expirationSeconds', type=d.T.integer)]),
  withExpirationSeconds(expirationSeconds): { expirationSeconds: expirationSeconds },
  '#withPath':: d.fn(help='Path is the path relative to the mount point of the file to project the token into.', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#mixin': 'ignore',
  mixin: self
}