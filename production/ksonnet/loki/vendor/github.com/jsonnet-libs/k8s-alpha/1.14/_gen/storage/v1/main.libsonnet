{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1', url='', help=''),
  storageClass: (import 'storageClass.libsonnet'),
  volumeAttachment: (import 'volumeAttachment.libsonnet'),
  volumeAttachmentSource: (import 'volumeAttachmentSource.libsonnet'),
  volumeAttachmentSpec: (import 'volumeAttachmentSpec.libsonnet'),
  volumeAttachmentStatus: (import 'volumeAttachmentStatus.libsonnet'),
  volumeError: (import 'volumeError.libsonnet')
}