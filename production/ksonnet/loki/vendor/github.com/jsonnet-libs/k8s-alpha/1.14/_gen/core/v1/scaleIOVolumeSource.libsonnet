{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='scaleIOVolumeSource', url='', help='ScaleIOVolumeSource represents a persistent ScaleIO volume'),
  '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
  secretRef: {
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secretRef+: { name: name } }
  },
  '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Default is "xfs".', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withGateway':: d.fn(help='The host address of the ScaleIO API Gateway.', args=[d.arg(name='gateway', type=d.T.string)]),
  withGateway(gateway): { gateway: gateway },
  '#withProtectionDomain':: d.fn(help='The name of the ScaleIO Protection Domain for the configured storage.', args=[d.arg(name='protectionDomain', type=d.T.string)]),
  withProtectionDomain(protectionDomain): { protectionDomain: protectionDomain },
  '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withSslEnabled':: d.fn(help='Flag to enable/disable SSL communication with Gateway, default false', args=[d.arg(name='sslEnabled', type=d.T.boolean)]),
  withSslEnabled(sslEnabled): { sslEnabled: sslEnabled },
  '#withStorageMode':: d.fn(help='Indicates whether the storage for a volume should be ThickProvisioned or ThinProvisioned. Default is ThinProvisioned.', args=[d.arg(name='storageMode', type=d.T.string)]),
  withStorageMode(storageMode): { storageMode: storageMode },
  '#withStoragePool':: d.fn(help='The ScaleIO Storage Pool associated with the protection domain.', args=[d.arg(name='storagePool', type=d.T.string)]),
  withStoragePool(storagePool): { storagePool: storagePool },
  '#withSystem':: d.fn(help='The name of the storage system as configured in ScaleIO.', args=[d.arg(name='system', type=d.T.string)]),
  withSystem(system): { system: system },
  '#withVolumeName':: d.fn(help='The name of a volume already created in the ScaleIO system that is associated with this volume source.', args=[d.arg(name='volumeName', type=d.T.string)]),
  withVolumeName(volumeName): { volumeName: volumeName },
  '#mixin': 'ignore',
  mixin: self
}