{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volumeAttachmentStatus', url='', help='VolumeAttachmentStatus is the status of a VolumeAttachment request.'),
  '#attachError':: d.obj(help='VolumeError captures an error encountered during a volume operation.'),
  attachError: {
    '#withMessage':: d.fn(help='String detailing the error encountered during Attach or Detach operation. This string maybe logged, so it should not contain sensitive information.', args=[d.arg(name='message', type=d.T.string)]),
    withMessage(message): { attachError+: { message: message } },
    '#withTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='time', type=d.T.string)]),
    withTime(time): { attachError+: { time: time } }
  },
  '#detachError':: d.obj(help='VolumeError captures an error encountered during a volume operation.'),
  detachError: {
    '#withMessage':: d.fn(help='String detailing the error encountered during Attach or Detach operation. This string maybe logged, so it should not contain sensitive information.', args=[d.arg(name='message', type=d.T.string)]),
    withMessage(message): { detachError+: { message: message } },
    '#withTime':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='time', type=d.T.string)]),
    withTime(time): { detachError+: { time: time } }
  },
  '#withAttached':: d.fn(help='Indicates the volume is successfully attached. This field must only be set by the entity completing the attach operation, i.e. the external-attacher.', args=[d.arg(name='attached', type=d.T.boolean)]),
  withAttached(attached): { attached: attached },
  '#withAttachmentMetadata':: d.fn(help='Upon successful attach, this field is populated with any information returned by the attach operation that must be passed into subsequent WaitForAttach or Mount calls. This field must only be set by the entity completing the attach operation, i.e. the external-attacher.', args=[d.arg(name='attachmentMetadata', type=d.T.object)]),
  withAttachmentMetadata(attachmentMetadata): { attachmentMetadata: attachmentMetadata },
  '#withAttachmentMetadataMixin':: d.fn(help='Upon successful attach, this field is populated with any information returned by the attach operation that must be passed into subsequent WaitForAttach or Mount calls. This field must only be set by the entity completing the attach operation, i.e. the external-attacher.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='attachmentMetadata', type=d.T.object)]),
  withAttachmentMetadataMixin(attachmentMetadata): { attachmentMetadata+: attachmentMetadata },
  '#mixin': 'ignore',
  mixin: self
}