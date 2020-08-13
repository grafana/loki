{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='portworxVolumeSource', url='', help='PortworxVolumeSource represents a Portworx volume resource.'),
  '#withFsType':: d.fn(help='FSType represents the filesystem type to mount Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withVolumeID':: d.fn(help='VolumeID uniquely identifies a Portworx volume', args=[d.arg(name='volumeID', type=d.T.string)]),
  withVolumeID(volumeID): { volumeID: volumeID },
  '#mixin': 'ignore',
  mixin: self
}