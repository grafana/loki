{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='downwardAPIProjection', url='', help='Represents downward API info for projecting into a projected volume. Note that this is identical to a downwardAPI volume source without the default mode.'),
  '#withItems':: d.fn(help='Items is a list of DownwardAPIVolume file', args=[d.arg(name='items', type=d.T.array)]),
  withItems(items): { items: if std.isArray(v=items) then items else [items] },
  '#withItemsMixin':: d.fn(help='Items is a list of DownwardAPIVolume file\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='items', type=d.T.array)]),
  withItemsMixin(items): { items+: if std.isArray(v=items) then items else [items] },
  '#mixin': 'ignore',
  mixin: self
}