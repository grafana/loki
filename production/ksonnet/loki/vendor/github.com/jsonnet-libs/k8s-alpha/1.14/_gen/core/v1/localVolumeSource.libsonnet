{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='localVolumeSource', url='', help='Local represents directly-attached storage with node affinity (Beta feature)'),
  '#withFsType':: d.fn(help='Filesystem type to mount. It applies only when the Path is a block device. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". The default value is to auto-select a fileystem if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withPath':: d.fn(help='The full path to the volume on the node. It can be either a directory or block device (disk, partition, ...).', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#mixin': 'ignore',
  mixin: self
}