{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='rbdPersistentVolumeSource', url='', help='Represents a Rados Block Device mount that lasts the lifetime of a pod. RBD volumes support ownership management and SELinux relabeling.'),
  '#secretRef':: d.obj(help='SecretReference represents a Secret Reference. It has enough information to retrieve secret in any namespace'),
  secretRef: {
    '#withName':: d.fn(help='Name is unique within a namespace to reference a secret resource.', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secretRef+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace defines the space within which the secret name must be unique.', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { secretRef+: { namespace: namespace } }
  },
  '#withFsType':: d.fn(help='Filesystem type of the volume that you want to mount. Tip: Ensure that the filesystem type is supported by the host operating system. Examples: "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified. More info: https://kubernetes.io/docs/concepts/storage/volumes#rbd', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withImage':: d.fn(help='The rados image name. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='image', type=d.T.string)]),
  withImage(image): { image: image },
  '#withKeyring':: d.fn(help='Keyring is the path to key ring for RBDUser. Default is /etc/ceph/keyring. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='keyring', type=d.T.string)]),
  withKeyring(keyring): { keyring: keyring },
  '#withMonitors':: d.fn(help='A collection of Ceph monitors. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='monitors', type=d.T.array)]),
  withMonitors(monitors): { monitors: if std.isArray(v=monitors) then monitors else [monitors] },
  '#withMonitorsMixin':: d.fn(help='A collection of Ceph monitors. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='monitors', type=d.T.array)]),
  withMonitorsMixin(monitors): { monitors+: if std.isArray(v=monitors) then monitors else [monitors] },
  '#withPool':: d.fn(help='The rados pool name. Default is rbd. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='pool', type=d.T.string)]),
  withPool(pool): { pool: pool },
  '#withReadOnly':: d.fn(help='ReadOnly here will force the ReadOnly setting in VolumeMounts. Defaults to false. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withUser':: d.fn(help='The rados user name. Default is admin. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='user', type=d.T.string)]),
  withUser(user): { user: user },
  '#mixin': 'ignore',
  mixin: self
}