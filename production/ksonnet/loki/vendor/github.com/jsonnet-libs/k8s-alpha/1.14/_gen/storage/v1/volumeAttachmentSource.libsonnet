{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeAttachmentSource', url='', help='VolumeAttachmentSource represents a volume that should be attached. Right now only PersistenVolumes can be attached via external attacher, in future we may allow also inline volumes in pods. Exactly one member can be set.'),
  '#withPersistentVolumeName':: d.fn(help='Name of the persistent volume to attach.', args=[d.arg(name='persistentVolumeName', type=d.T.string)]),
  withPersistentVolumeName(persistentVolumeName): { persistentVolumeName: persistentVolumeName },
  '#mixin': 'ignore',
  mixin: self
}