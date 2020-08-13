{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeDevice', url='', help='volumeDevice describes a mapping of a raw block device within a container.'),
  '#withDevicePath':: d.fn(help='devicePath is the path inside of the container that the device will be mapped to.', args=[d.arg(name='devicePath', type=d.T.string)]),
  withDevicePath(devicePath): { devicePath: devicePath },
  '#withName':: d.fn(help='name must match the name of a persistentVolumeClaim in the pod', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#mixin': 'ignore',
  mixin: self
}