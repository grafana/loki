{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='csiNodeSpec', url='', help='CSINodeSpec holds information about the specification of all CSI drivers installed on a node'),
  '#withDrivers':: d.fn(help='drivers is a list of information of all CSI Drivers existing on a node. If all drivers in the list are uninstalled, this can become empty.', args=[d.arg(name='drivers', type=d.T.array)]),
  withDrivers(drivers): { drivers: if std.isArray(v=drivers) then drivers else [drivers] },
  '#withDriversMixin':: d.fn(help='drivers is a list of information of all CSI Drivers existing on a node. If all drivers in the list are uninstalled, this can become empty.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='drivers', type=d.T.array)]),
  withDriversMixin(drivers): { drivers+: if std.isArray(v=drivers) then drivers else [drivers] },
  '#mixin': 'ignore',
  mixin: self
}