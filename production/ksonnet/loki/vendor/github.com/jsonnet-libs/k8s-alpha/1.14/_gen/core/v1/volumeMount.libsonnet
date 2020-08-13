{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeMount', url='', help='VolumeMount describes a mounting of a Volume within a container.'),
  '#withMountPath':: d.fn(help="Path within the container at which the volume should be mounted.  Must not contain ':'.", args=[d.arg(name='mountPath', type=d.T.string)]),
  withMountPath(mountPath): { mountPath: mountPath },
  '#withMountPropagation':: d.fn(help='mountPropagation determines how mounts are propagated from the host to container and the other way around. When not set, MountPropagationNone is used. This field is beta in 1.10.', args=[d.arg(name='mountPropagation', type=d.T.string)]),
  withMountPropagation(mountPropagation): { mountPropagation: mountPropagation },
  '#withName':: d.fn(help='This must match the Name of a Volume.', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withReadOnly':: d.fn(help='Mounted read-only if true, read-write otherwise (false or unspecified). Defaults to false.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withSubPath':: d.fn(help="Path within the volume from which the container's volume should be mounted. Defaults to '' (volume's root).", args=[d.arg(name='subPath', type=d.T.string)]),
  withSubPath(subPath): { subPath: subPath },
  '#withSubPathExpr':: d.fn(help="Expanded path within the volume from which the container's volume should be mounted. Behaves similarly to SubPath but environment variable references $(VAR_NAME) are expanded using the container's environment. Defaults to '' (volume's root). SubPathExpr and SubPath are mutually exclusive. This field is alpha in 1.14.", args=[d.arg(name='subPathExpr', type=d.T.string)]),
  withSubPathExpr(subPathExpr): { subPathExpr: subPathExpr },
  '#mixin': 'ignore',
  mixin: self
}