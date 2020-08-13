{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='downwardAPIVolumeSource', url='', help='DownwardAPIVolumeSource represents a volume containing downward API info. Downward API volumes support ownership management and SELinux relabeling.'),
  '#withDefaultMode':: d.fn(help='Optional: mode bits to use on created files by default. Must be a value between 0 and 0777. Defaults to 0644. Directories within the path are not affected by this setting. This might be in conflict with other options that affect the file mode, like fsGroup, and the result can be other mode bits set.', args=[d.arg(name='defaultMode', type=d.T.integer)]),
  withDefaultMode(defaultMode): { defaultMode: defaultMode },
  '#withItems':: d.fn(help='Items is a list of downward API volume file', args=[d.arg(name='items', type=d.T.array)]),
  withItems(items): { items: if std.isArray(v=items) then items else [items] },
  '#withItemsMixin':: d.fn(help='Items is a list of downward API volume file\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='items', type=d.T.array)]),
  withItemsMixin(items): { items+: if std.isArray(v=items) then items else [items] },
  '#mixin': 'ignore',
  mixin: self
}