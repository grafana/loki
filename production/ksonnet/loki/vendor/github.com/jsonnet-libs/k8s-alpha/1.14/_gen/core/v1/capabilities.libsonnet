{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='capabilities', url='', help='Adds and removes POSIX capabilities from running containers.'),
  '#withAdd':: d.fn(help='Added capabilities', args=[d.arg(name='add', type=d.T.array)]),
  withAdd(add): { add: if std.isArray(v=add) then add else [add] },
  '#withAddMixin':: d.fn(help='Added capabilities\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='add', type=d.T.array)]),
  withAddMixin(add): { add+: if std.isArray(v=add) then add else [add] },
  '#withDrop':: d.fn(help='Removed capabilities', args=[d.arg(name='drop', type=d.T.array)]),
  withDrop(drop): { drop: if std.isArray(v=drop) then drop else [drop] },
  '#withDropMixin':: d.fn(help='Removed capabilities\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='drop', type=d.T.array)]),
  withDropMixin(drop): { drop+: if std.isArray(v=drop) then drop else [drop] },
  '#mixin': 'ignore',
  mixin: self
}