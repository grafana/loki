{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='csiPersistentVolumeSource', url='', help='Represents storage that is managed by an external CSI volume driver (Beta feature)'),
  '#controllerPublishSecretRef':: d.obj(help='SecretReference represents a Secret Reference. It has enough information to retrieve secret in any namespace'),
  controllerPublishSecretRef: {
    '#withName':: d.fn(help='Name is unique within a namespace to reference a secret resource.', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { controllerPublishSecretRef+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace defines the space within which the secret name must be unique.', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { controllerPublishSecretRef+: { namespace: namespace } }
  },
  '#nodePublishSecretRef':: d.obj(help='SecretReference represents a Secret Reference. It has enough information to retrieve secret in any namespace'),
  nodePublishSecretRef: {
    '#withName':: d.fn(help='Name is unique within a namespace to reference a secret resource.', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { nodePublishSecretRef+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace defines the space within which the secret name must be unique.', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { nodePublishSecretRef+: { namespace: namespace } }
  },
  '#nodeStageSecretRef':: d.obj(help='SecretReference represents a Secret Reference. It has enough information to retrieve secret in any namespace'),
  nodeStageSecretRef: {
    '#withName':: d.fn(help='Name is unique within a namespace to reference a secret resource.', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { nodeStageSecretRef+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace defines the space within which the secret name must be unique.', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { nodeStageSecretRef+: { namespace: namespace } }
  },
  '#withDriver':: d.fn(help='Driver is the name of the driver to use for this volume. Required.', args=[d.arg(name='driver', type=d.T.string)]),
  withDriver(driver): { driver: driver },
  '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs".', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withReadOnly':: d.fn(help='Optional: The value to pass to ControllerPublishVolumeRequest. Defaults to false (read/write).', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withVolumeAttributes':: d.fn(help='Attributes of the volume to publish.', args=[d.arg(name='volumeAttributes', type=d.T.object)]),
  withVolumeAttributes(volumeAttributes): { volumeAttributes: volumeAttributes },
  '#withVolumeAttributesMixin':: d.fn(help='Attributes of the volume to publish.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='volumeAttributes', type=d.T.object)]),
  withVolumeAttributesMixin(volumeAttributes): { volumeAttributes+: volumeAttributes },
  '#withVolumeHandle':: d.fn(help='VolumeHandle is the unique volume name returned by the CSI volume plugin’s CreateVolume to refer to the volume on all subsequent calls. Required.', args=[d.arg(name='volumeHandle', type=d.T.string)]),
  withVolumeHandle(volumeHandle): { volumeHandle: volumeHandle },
  '#mixin': 'ignore',
  mixin: self
}