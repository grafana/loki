{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1alpha1', url='', help=''),
  volumeAttachment: (import 'volumeAttachment.libsonnet'),
  volumeAttachmentSource: (import 'volumeAttachmentSource.libsonnet'),
  volumeAttachmentSpec: (import 'volumeAttachmentSpec.libsonnet'),
  volumeAttachmentStatus: (import 'volumeAttachmentStatus.libsonnet'),
  volumeError: (import 'volumeError.libsonnet')
}