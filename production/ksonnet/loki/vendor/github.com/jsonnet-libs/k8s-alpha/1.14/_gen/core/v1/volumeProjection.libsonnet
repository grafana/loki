{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeProjection', url='', help='Projection that may be projected along with other supported volume types'),
  '#configMap':: d.obj(help="Adapts a ConfigMap into a projected volume.\n\nThe contents of the target ConfigMap's Data field will be presented in a projected volume as files using the keys in the Data field as the file names, unless the items element is populated with specific mappings of keys to paths. Note that this is identical to a configmap volume source without the default mode."),
  configMap: {
    '#withItems':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced ConfigMap will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the ConfigMap, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.", args=[d.arg(name='items', type=d.T.array)]),
    withItems(items): { configMap+: { items: if std.isArray(v=items) then items else [items] } },
    '#withItemsMixin':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced ConfigMap will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the ConfigMap, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='items', type=d.T.array)]),
    withItemsMixin(items): { configMap+: { items+: if std.isArray(v=items) then items else [items] } },
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { configMap+: { name: name } },
    '#withOptional':: d.fn(help="Specify whether the ConfigMap or it's keys must be defined", args=[d.arg(name='optional', type=d.T.boolean)]),
    withOptional(optional): { configMap+: { optional: optional } }
  },
  '#downwardAPI':: d.obj(help='Represents downward API info for projecting into a projected volume. Note that this is identical to a downwardAPI volume source without the default mode.'),
  downwardAPI: {
    '#withItems':: d.fn(help='Items is a list of DownwardAPIVolume file', args=[d.arg(name='items', type=d.T.array)]),
    withItems(items): { downwardAPI+: { items: if std.isArray(v=items) then items else [items] } },
    '#withItemsMixin':: d.fn(help='Items is a list of DownwardAPIVolume file\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='items', type=d.T.array)]),
    withItemsMixin(items): { downwardAPI+: { items+: if std.isArray(v=items) then items else [items] } }
  },
  '#secret':: d.obj(help="Adapts a secret into a projected volume.\n\nThe contents of the target Secret's Data field will be presented in a projected volume as files using the keys in the Data field as the file names. Note that this is identical to a secret volume source without the default mode."),
  secret: {
    '#withItems':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced Secret will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the Secret, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.", args=[d.arg(name='items', type=d.T.array)]),
    withItems(items): { secret+: { items: if std.isArray(v=items) then items else [items] } },
    '#withItemsMixin':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced Secret will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the Secret, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='items', type=d.T.array)]),
    withItemsMixin(items): { secret+: { items+: if std.isArray(v=items) then items else [items] } },
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secret+: { name: name } },
    '#withOptional':: d.fn(help='Specify whether the Secret or its key must be defined', args=[d.arg(name='optional', type=d.T.boolean)]),
    withOptional(optional): { secret+: { optional: optional } }
  },
  '#serviceAccountToken':: d.obj(help='ServiceAccountTokenProjection represents a projected service account token volume. This projection can be used to insert a service account token into the pods runtime filesystem for use against APIs (Kubernetes API Server or otherwise).'),
  serviceAccountToken: {
    '#withAudience':: d.fn(help='Audience is the intended audience of the token. A recipient of a token must identify itself with an identifier specified in the audience of the token, and otherwise should reject the token. The audience defaults to the identifier of the apiserver.', args=[d.arg(name='audience', type=d.T.string)]),
    withAudience(audience): { serviceAccountToken+: { audience: audience } },
    '#withExpirationSeconds':: d.fn(help='ExpirationSeconds is the requested duration of validity of the service account token. As the token approaches expiration, the kubelet volume plugin will proactively rotate the service account token. The kubelet will start trying to rotate the token if the token is older than 80 percent of its time to live or if the token is older than 24 hours.Defaults to 1 hour and must be at least 10 minutes.', args=[d.arg(name='expirationSeconds', type=d.T.integer)]),
    withExpirationSeconds(expirationSeconds): { serviceAccountToken+: { expirationSeconds: expirationSeconds } },
    '#withPath':: d.fn(help='Path is the path relative to the mount point of the file to project the token into.', args=[d.arg(name='path', type=d.T.string)]),
    withPath(path): { serviceAccountToken+: { path: path } }
  },
  '#mixin': 'ignore',
  mixin: self
}