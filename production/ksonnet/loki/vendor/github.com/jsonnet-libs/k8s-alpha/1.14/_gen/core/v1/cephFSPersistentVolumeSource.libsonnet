{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='cephFSPersistentVolumeSource', url='', help='Represents a Ceph Filesystem mount that lasts the lifetime of a pod Cephfs volumes do not support ownership management or SELinux relabeling.'),
  '#secretRef':: d.obj(help='SecretReference represents a Secret Reference. It has enough information to retrieve secret in any namespace'),
  secretRef: {
    '#withName':: d.fn(help='Name is unique within a namespace to reference a secret resource.', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secretRef+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace defines the space within which the secret name must be unique.', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { secretRef+: { namespace: namespace } }
  },
  '#withMonitors':: d.fn(help='Required: Monitors is a collection of Ceph monitors More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='monitors', type=d.T.array)]),
  withMonitors(monitors): { monitors: if std.isArray(v=monitors) then monitors else [monitors] },
  '#withMonitorsMixin':: d.fn(help='Required: Monitors is a collection of Ceph monitors More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='monitors', type=d.T.array)]),
  withMonitorsMixin(monitors): { monitors+: if std.isArray(v=monitors) then monitors else [monitors] },
  '#withPath':: d.fn(help='Optional: Used as the mounted root, rather than the full Ceph tree, default is /', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#withReadOnly':: d.fn(help='Optional: Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts. More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withSecretFile':: d.fn(help='Optional: SecretFile is the path to key ring for User, default is /etc/ceph/user.secret More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='secretFile', type=d.T.string)]),
  withSecretFile(secretFile): { secretFile: secretFile },
  '#withUser':: d.fn(help='Optional: User is the rados user name, default is admin More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='user', type=d.T.string)]),
  withUser(user): { user: user },
  '#mixin': 'ignore',
  mixin: self
}