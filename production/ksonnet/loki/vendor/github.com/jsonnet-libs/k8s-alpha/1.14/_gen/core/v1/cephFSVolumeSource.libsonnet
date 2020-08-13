{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='cephFSVolumeSource', url='', help='Represents a Ceph Filesystem mount that lasts the lifetime of a pod Cephfs volumes do not support ownership management or SELinux relabeling.'),
  '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
  secretRef: {
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secretRef+: { name: name } }
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