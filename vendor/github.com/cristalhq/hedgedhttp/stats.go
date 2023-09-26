package hedgedhttp

import "sync/atomic"

// atomicCounter is a false sharing safe counter.
type atomicCounter struct {
	count uint64
	_     [7]uint64
}

type cacheLine [64]byte

// Stats object that can be queried to obtain certain metrics and get better observability.
type Stats struct {
	_                        cacheLine
	requestedRoundTrips      atomicCounter
	actualRoundTrips         atomicCounter
	failedRoundTrips         atomicCounter
	canceledByUserRoundTrips atomicCounter
	canceledSubRequests      atomicCounter
	_                        cacheLine
}

func (s *Stats) requestedRoundTripsInc()      { atomic.AddUint64(&s.requestedRoundTrips.count, 1) }
func (s *Stats) actualRoundTripsInc()         { atomic.AddUint64(&s.actualRoundTrips.count, 1) }
func (s *Stats) failedRoundTripsInc()         { atomic.AddUint64(&s.failedRoundTrips.count, 1) }
func (s *Stats) canceledByUserRoundTripsInc() { atomic.AddUint64(&s.canceledByUserRoundTrips.count, 1) }
func (s *Stats) canceledSubRequestsInc()      { atomic.AddUint64(&s.canceledSubRequests.count, 1) }

// RequestedRoundTrips returns count of requests that were requested by client.
func (s *Stats) RequestedRoundTrips() uint64 {
	return atomic.LoadUint64(&s.requestedRoundTrips.count)
}

// ActualRoundTrips returns count of requests that were actually sent.
func (s *Stats) ActualRoundTrips() uint64 {
	return atomic.LoadUint64(&s.actualRoundTrips.count)
}

// FailedRoundTrips returns count of requests that failed.
func (s *Stats) FailedRoundTrips() uint64 {
	return atomic.LoadUint64(&s.failedRoundTrips.count)
}

// CanceledByUserRoundTrips returns count of requests that were canceled by user, using request context.
func (s *Stats) CanceledByUserRoundTrips() uint64 {
	return atomic.LoadUint64(&s.canceledByUserRoundTrips.count)
}

// CanceledSubRequests returns count of hedged sub-requests that were canceled by transport.
func (s *Stats) CanceledSubRequests() uint64 {
	return atomic.LoadUint64(&s.canceledSubRequests.count)
}

// StatsSnapshot is a snapshot of Stats.
type StatsSnapshot struct {
	RequestedRoundTrips      uint64 // count of requests that were requested by client
	ActualRoundTrips         uint64 // count of requests that were actually sent
	FailedRoundTrips         uint64 // count of requests that failed
	CanceledByUserRoundTrips uint64 // count of requests that were canceled by user, using request context
	CanceledSubRequests      uint64 // count of hedged sub-requests that were canceled by transport
}

// Snapshot of the stats.
func (s *Stats) Snapshot() StatsSnapshot {
	return StatsSnapshot{
		RequestedRoundTrips:      s.RequestedRoundTrips(),
		ActualRoundTrips:         s.ActualRoundTrips(),
		FailedRoundTrips:         s.FailedRoundTrips(),
		CanceledByUserRoundTrips: s.CanceledByUserRoundTrips(),
		CanceledSubRequests:      s.CanceledSubRequests(),
	}
}
