{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1beta1', url='', help=''),
  csiDriver: (import 'csiDriver.libsonnet'),
  csiDriverSpec: (import 'csiDriverSpec.libsonnet'),
  csiNode: (import 'csiNode.libsonnet'),
  csiNodeDriver: (import 'csiNodeDriver.libsonnet'),
  csiNodeSpec: (import 'csiNodeSpec.libsonnet'),
  storageClass: (import 'storageClass.libsonnet'),
  volumeAttachment: (import 'volumeAttachment.libsonnet'),
  volumeAttachmentSource: (import 'volumeAttachmentSource.libsonnet'),
  volumeAttachmentSpec: (import 'volumeAttachmentSpec.libsonnet'),
  volumeAttachmentStatus: (import 'volumeAttachmentStatus.libsonnet'),
  volumeError: (import 'volumeError.libsonnet')
}