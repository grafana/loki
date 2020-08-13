{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='csiVolumeSource', url='', help='Represents a source location of a volume to mount, managed by an external CSI driver'),
  '#nodePublishSecretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
  nodePublishSecretRef: {
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { nodePublishSecretRef+: { name: name } }
  },
  '#withDriver':: d.fn(help='Driver is the name of the CSI driver that handles this volume. Consult with your admin for the correct name as registered in the cluster.', args=[d.arg(name='driver', type=d.T.string)]),
  withDriver(driver): { driver: driver },
  '#withFsType':: d.fn(help='Filesystem type to mount. Ex. "ext4", "xfs", "ntfs". If not provided, the empty value is passed to the associated CSI driver which will determine the default filesystem to apply.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withReadOnly':: d.fn(help='Specifies a read-only configuration for the volume. Defaults to false (read/write).', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withVolumeAttributes':: d.fn(help="VolumeAttributes stores driver-specific properties that are passed to the CSI driver. Consult your driver's documentation for supported values.", args=[d.arg(name='volumeAttributes', type=d.T.object)]),
  withVolumeAttributes(volumeAttributes): { volumeAttributes: volumeAttributes },
  '#withVolumeAttributesMixin':: d.fn(help="VolumeAttributes stores driver-specific properties that are passed to the CSI driver. Consult your driver's documentation for supported values.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='volumeAttributes', type=d.T.object)]),
  withVolumeAttributesMixin(volumeAttributes): { volumeAttributes+: volumeAttributes },
  '#mixin': 'ignore',
  mixin: self
}