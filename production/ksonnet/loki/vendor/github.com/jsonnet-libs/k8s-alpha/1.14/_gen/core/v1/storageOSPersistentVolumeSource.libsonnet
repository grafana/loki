{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='storageOSPersistentVolumeSource', url='', help='Represents a StorageOS persistent volume resource.'),
  '#secretRef':: d.obj(help='ObjectReference contains enough information to let you inspect or modify the referred object.'),
  secretRef: {
    '#withFieldPath':: d.fn(help='If referring to a piece of an object instead of an entire object, this string should contain a valid JSON/Go field access statement, such as desiredState.manifest.containers[2]. For example, if the object reference is to a container within a pod, this would take on a value like: "spec.containers{name}" (where "name" refers to the name of the container that triggered the event) or if no container name is specified "spec.containers[2]" (container with index 2 in this pod). This syntax is chosen only to have some well-defined way of referencing a part of an object.', args=[d.arg(name='fieldPath', type=d.T.string)]),
    withFieldPath(fieldPath): { secretRef+: { fieldPath: fieldPath } },
    '#withKind':: d.fn(help='Kind of the referent. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds', args=[d.arg(name='kind', type=d.T.string)]),
    withKind(kind): { secretRef+: { kind: kind } },
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { secretRef+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { secretRef+: { namespace: namespace } },
    '#withResourceVersion':: d.fn(help='Specific resourceVersion to which this reference is made, if any. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#concurrency-control-and-consistency', args=[d.arg(name='resourceVersion', type=d.T.string)]),
    withResourceVersion(resourceVersion): { secretRef+: { resourceVersion: resourceVersion } },
    '#withUid':: d.fn(help='UID of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#uids', args=[d.arg(name='uid', type=d.T.string)]),
    withUid(uid): { secretRef+: { uid: uid } }
  },
  '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withVolumeName':: d.fn(help='VolumeName is the human-readable name of the StorageOS volume.  Volume names are only unique within a namespace.', args=[d.arg(name='volumeName', type=d.T.string)]),
  withVolumeName(volumeName): { volumeName: volumeName },
  '#withVolumeNamespace':: d.fn(help="VolumeNamespace specifies the scope of the volume within StorageOS.  If no namespace is specified then the Pod's namespace will be used.  This allows the Kubernetes name scoping to be mirrored within StorageOS for tighter integration. Set VolumeName to any name to override the default behaviour. Set to 'default' if you are not using namespaces within StorageOS. Namespaces that do not pre-exist within StorageOS will be created.", args=[d.arg(name='volumeNamespace', type=d.T.string)]),
  withVolumeNamespace(volumeNamespace): { volumeNamespace: volumeNamespace },
  '#mixin': 'ignore',
  mixin: self
}