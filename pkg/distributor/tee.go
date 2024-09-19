package distributor

// Tee implementations can duplicate the log streams to another endpoint.
type Tee interface {
	Duplicate(tenant string, streams []KeyedStream)
}

type TrackedTee interface {
	DuplicateWithTracking(tenantID string, streams []KeyedStream, tracker *PushTracker)
}

// WrapTee wraps a new Tee around an existing Tee.
func WrapTee(existing, new Tee) Tee {
	if existing == nil {
		return new
	}
	if multi, ok := existing.(*multiTee); ok {
		return &multiTee{append(multi.tees, new)}
	}
	return &multiTee{tees: []Tee{existing, new}}
}

type multiTee struct {
	tees []Tee
}

func (m *multiTee) Duplicate(tenant string, streams []KeyedStream) {
	for _, tee := range m.tees {
		tee.Duplicate(tenant, streams)
	}
}

type multiTrackedTee struct {
	tees []TrackedTee
}

// WrapTrackedTee wraps a new TrackedTee around an existing TrackedTee.
func WrapTrackedTee(existing, new TrackedTee) TrackedTee {
	if existing == nil {
		return new
	}
	if multi, ok := existing.(*multiTrackedTee); ok {
		return &multiTrackedTee{append(multi.tees, new)}
	}
	return &multiTrackedTee{tees: []TrackedTee{existing, new}}
}

func (m *multiTrackedTee) DuplicateWithTracking(tenant string, streams []KeyedStream, tracker *PushTracker) {
	tracker.StreamsPending.Add(int32(len(streams) * len(m.tees)))
	for _, tee := range m.tees {
		go tee.DuplicateWithTracking(tenant, streams, tracker)
	}
}
