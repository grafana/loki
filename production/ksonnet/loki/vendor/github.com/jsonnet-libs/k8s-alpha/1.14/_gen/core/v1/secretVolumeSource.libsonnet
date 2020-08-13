{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='secretVolumeSource', url='', help="Adapts a Secret into a volume.\n\nThe contents of the target Secret's Data field will be presented in a volume as files using the keys in the Data field as the file names. Secret volumes support ownership management and SELinux relabeling."),
  '#withDefaultMode':: d.fn(help='Optional: mode bits to use on created files by default. Must be a value between 0 and 0777. Defaults to 0644. Directories within the path are not affected by this setting. This might be in conflict with other options that affect the file mode, like fsGroup, and the result can be other mode bits set.', args=[d.arg(name='defaultMode', type=d.T.integer)]),
  withDefaultMode(defaultMode): { defaultMode: defaultMode },
  '#withItems':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced Secret will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the Secret, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.", args=[d.arg(name='items', type=d.T.array)]),
  withItems(items): { items: if std.isArray(v=items) then items else [items] },
  '#withItemsMixin':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced Secret will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the Secret, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='items', type=d.T.array)]),
  withItemsMixin(items): { items+: if std.isArray(v=items) then items else [items] },
  '#withOptional':: d.fn(help="Specify whether the Secret or it's keys must be defined", args=[d.arg(name='optional', type=d.T.boolean)]),
  withOptional(optional): { optional: optional },
  '#withSecretName':: d.fn(help="Name of the secret in the pod's namespace to use. More info: https://kubernetes.io/docs/concepts/storage/volumes#secret", args=[d.arg(name='secretName', type=d.T.string)]),
  withSecretName(secretName): { secretName: secretName },
  '#mixin': 'ignore',
  mixin: self
}