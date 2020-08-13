{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='persistentVolumeStatus', url='', help='PersistentVolumeStatus is the current status of a persistent volume.'),
  '#withMessage':: d.fn(help='A human-readable message indicating details about why the volume is in this state.', args=[d.arg(name='message', type=d.T.string)]),
  withMessage(message): { message: message },
  '#withPhase':: d.fn(help='Phase indicates if a volume is available, bound to a claim, or released by a claim. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#phase', args=[d.arg(name='phase', type=d.T.string)]),
  withPhase(phase): { phase: phase },
  '#withReason':: d.fn(help='Reason is a brief CamelCase string that describes any failure and is meant for machine parsing and tidy display in the CLI.', args=[d.arg(name='reason', type=d.T.string)]),
  withReason(reason): { reason: reason },
  '#mixin': 'ignore',
  mixin: self
}