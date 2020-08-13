{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='quobyteVolumeSource', url='', help='Represents a Quobyte mount that lasts the lifetime of a pod. Quobyte volumes do not support ownership management or SELinux relabeling.'),
  '#withGroup':: d.fn(help='Group to map volume access to Default is no group', args=[d.arg(name='group', type=d.T.string)]),
  withGroup(group): { group: group },
  '#withReadOnly':: d.fn(help='ReadOnly here will force the Quobyte volume to be mounted with read-only permissions. Defaults to false.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withRegistry':: d.fn(help='Registry represents a single or multiple Quobyte Registry services specified as a string as host:port pair (multiple entries are separated with commas) which acts as the central registry for volumes', args=[d.arg(name='registry', type=d.T.string)]),
  withRegistry(registry): { registry: registry },
  '#withTenant':: d.fn(help='Tenant owning the given Quobyte volume in the Backend Used with dynamically provisioned Quobyte volumes, value is set by the plugin', args=[d.arg(name='tenant', type=d.T.string)]),
  withTenant(tenant): { tenant: tenant },
  '#withUser':: d.fn(help='User to map volume access to Defaults to serivceaccount user', args=[d.arg(name='user', type=d.T.string)]),
  withUser(user): { user: user },
  '#withVolume':: d.fn(help='Volume is a string that references an already created Quobyte volume by name.', args=[d.arg(name='volume', type=d.T.string)]),
  withVolume(volume): { volume: volume },
  '#mixin': 'ignore',
  mixin: self
}