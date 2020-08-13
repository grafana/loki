{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='hostPathVolumeSource', url='', help='Represents a host path mapped into a pod. Host path volumes do not support ownership management or SELinux relabeling.'),
  '#withPath':: d.fn(help='Path of the directory on the host. If the path is a symlink, it will follow the link to the real path. More info: https://kubernetes.io/docs/concepts/storage/volumes#hostpath', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#withType':: d.fn(help='Type for HostPath Volume Defaults to "" More info: https://kubernetes.io/docs/concepts/storage/volumes#hostpath', args=[d.arg(name='type', type=d.T.string)]),
  withType(type): { type: type },
  '#mixin': 'ignore',
  mixin: self
}