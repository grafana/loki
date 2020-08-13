{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='leaseSpec', url='', help='LeaseSpec is a specification of a Lease.'),
  '#withAcquireTime':: d.fn(help='MicroTime is version of Time with microsecond level precision.', args=[d.arg(name='acquireTime', type=d.T.string)]),
  withAcquireTime(acquireTime): { acquireTime: acquireTime },
  '#withHolderIdentity':: d.fn(help='holderIdentity contains the identity of the holder of a current lease.', args=[d.arg(name='holderIdentity', type=d.T.string)]),
  withHolderIdentity(holderIdentity): { holderIdentity: holderIdentity },
  '#withLeaseDurationSeconds':: d.fn(help='leaseDurationSeconds is a duration that candidates for a lease need to wait to force acquire it. This is measure against time of last observed RenewTime.', args=[d.arg(name='leaseDurationSeconds', type=d.T.integer)]),
  withLeaseDurationSeconds(leaseDurationSeconds): { leaseDurationSeconds: leaseDurationSeconds },
  '#withLeaseTransitions':: d.fn(help='leaseTransitions is the number of transitions of a lease between holders.', args=[d.arg(name='leaseTransitions', type=d.T.integer)]),
  withLeaseTransitions(leaseTransitions): { leaseTransitions: leaseTransitions },
  '#withRenewTime':: d.fn(help='MicroTime is version of Time with microsecond level precision.', args=[d.arg(name='renewTime', type=d.T.string)]),
  withRenewTime(renewTime): { renewTime: renewTime },
  '#mixin': 'ignore',
  mixin: self
}