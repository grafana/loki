{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='attachedVolume', url='', help='AttachedVolume describes a volume attached to a node'),
  '#withDevicePath':: d.fn(help='DevicePath represents the device path where the volume should be available', args=[d.arg(name='devicePath', type=d.T.string)]),
  withDevicePath(devicePath): { devicePath: devicePath },
  '#withName':: d.fn(help='Name of the attached volume', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#mixin': 'ignore',
  mixin: self
}