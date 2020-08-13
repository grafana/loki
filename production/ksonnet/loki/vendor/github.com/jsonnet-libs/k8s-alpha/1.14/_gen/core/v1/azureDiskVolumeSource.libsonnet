{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='azureDiskVolumeSource', url='', help='AzureDisk represents an Azure Data Disk mount on the host and bind mount to the pod.'),
  '#withCachingMode':: d.fn(help='Host Caching mode: None, Read Only, Read Write.', args=[d.arg(name='cachingMode', type=d.T.string)]),
  withCachingMode(cachingMode): { cachingMode: cachingMode },
  '#withDiskName':: d.fn(help='The Name of the data disk in the blob storage', args=[d.arg(name='diskName', type=d.T.string)]),
  withDiskName(diskName): { diskName: diskName },
  '#withDiskURI':: d.fn(help='The URI the data disk in the blob storage', args=[d.arg(name='diskURI', type=d.T.string)]),
  withDiskURI(diskURI): { diskURI: diskURI },
  '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withKind':: d.fn(help='Expected values Shared: multiple blob disks per storage account  Dedicated: single blob disk per storage account  Managed: azure managed data disk (only in managed availability set). defaults to shared', args=[d.arg(name='kind', type=d.T.string)]),
  withKind(kind): { kind: kind },
  '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#mixin': 'ignore',
  mixin: self
}