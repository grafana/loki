{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeAttachmentSpec', url='', help='VolumeAttachmentSpec is the specification of a VolumeAttachment request.'),
  '#source':: d.obj(help='VolumeAttachmentSource represents a volume that should be attached. Right now only PersistenVolumes can be attached via external attacher, in future we may allow also inline volumes in pods. Exactly one member can be set.'),
  source: {
    '#withPersistentVolumeName':: d.fn(help='Name of the persistent volume to attach.', args=[d.arg(name='persistentVolumeName', type=d.T.string)]),
    withPersistentVolumeName(persistentVolumeName): { source+: { persistentVolumeName: persistentVolumeName } }
  },
  '#withAttacher':: d.fn(help='Attacher indicates the name of the volume driver that MUST handle this request. This is the name returned by GetPluginName().', args=[d.arg(name='attacher', type=d.T.string)]),
  withAttacher(attacher): { attacher: attacher },
  '#withNodeName':: d.fn(help='The node that the volume should be attached to.', args=[d.arg(name='nodeName', type=d.T.string)]),
  withNodeName(nodeName): { nodeName: nodeName },
  '#mixin': 'ignore',
  mixin: self
}