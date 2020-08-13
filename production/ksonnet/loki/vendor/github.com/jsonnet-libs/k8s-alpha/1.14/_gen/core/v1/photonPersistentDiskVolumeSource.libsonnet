{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='photonPersistentDiskVolumeSource', url='', help='Represents a Photon Controller persistent disk resource.'),
  '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withPdID':: d.fn(help='ID that identifies Photon Controller persistent disk', args=[d.arg(name='pdID', type=d.T.string)]),
  withPdID(pdID): { pdID: pdID },
  '#mixin': 'ignore',
  mixin: self
}