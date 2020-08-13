{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='vsphereVirtualDiskVolumeSource', url='', help='Represents a vSphere volume resource.'),
  '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withStoragePolicyID':: d.fn(help='Storage Policy Based Management (SPBM) profile ID associated with the StoragePolicyName.', args=[d.arg(name='storagePolicyID', type=d.T.string)]),
  withStoragePolicyID(storagePolicyID): { storagePolicyID: storagePolicyID },
  '#withStoragePolicyName':: d.fn(help='Storage Policy Based Management (SPBM) profile name.', args=[d.arg(name='storagePolicyName', type=d.T.string)]),
  withStoragePolicyName(storagePolicyName): { storagePolicyName: storagePolicyName },
  '#withVolumePath':: d.fn(help='Path that identifies vSphere volume vmdk', args=[d.arg(name='volumePath', type=d.T.string)]),
  withVolumePath(volumePath): { volumePath: volumePath },
  '#mixin': 'ignore',
  mixin: self
}