{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='flexVolumeSource', url='', help='FlexVolume represents a generic volume resource that is provisioned/attached using an exec based plugin.'),
  '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
  secretRef: {
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secretRef+: { name: name } }
  },
  '#withDriver':: d.fn(help='Driver is the name of the driver to use for this volume.', args=[d.arg(name='driver', type=d.T.string)]),
  withDriver(driver): { driver: driver },
  '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". The default filesystem depends on FlexVolume script.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withOptions':: d.fn(help='Optional: Extra command options if any.', args=[d.arg(name='options', type=d.T.object)]),
  withOptions(options): { options: options },
  '#withOptionsMixin':: d.fn(help='Optional: Extra command options if any.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='options', type=d.T.object)]),
  withOptionsMixin(options): { options+: options },
  '#withReadOnly':: d.fn(help='Optional: Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#mixin': 'ignore',
  mixin: self
}