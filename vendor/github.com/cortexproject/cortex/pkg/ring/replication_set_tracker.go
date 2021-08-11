package ring

type replicationSetResultTracker interface {
	// Signals an instance has done the execution, either successful (no error)
	// or failed (with error).
	done(instance *InstanceDesc, err error)

	// Returns true if the minimum number of successful results have been received.
	succeeded() bool

	// Returns true if the maximum number of failed executions have been reached.
	failed() bool
}

type defaultResultTracker struct {
	minSucceeded int
	numSucceeded int
	numErrors    int
	maxErrors    int
}

func newDefaultResultTracker(instances []InstanceDesc, maxErrors int) *defaultResultTracker {
	return &defaultResultTracker{
		minSucceeded: len(instances) - maxErrors,
		numSucceeded: 0,
		numErrors:    0,
		maxErrors:    maxErrors,
	}
}

func (t *defaultResultTracker) done(_ *InstanceDesc, err error) {
	if err == nil {
		t.numSucceeded++
	} else {
		t.numErrors++
	}
}

func (t *defaultResultTracker) succeeded() bool {
	return t.numSucceeded >= t.minSucceeded
}

func (t *defaultResultTracker) failed() bool {
	return t.numErrors > t.maxErrors
}

// zoneAwareResultTracker tracks the results per zone.
// All instances in a zone must succeed in order for the zone to succeed.
type zoneAwareResultTracker struct {
	waitingByZone       map[string]int
	failuresByZone      map[string]int
	minSuccessfulZones  int
	maxUnavailableZones int
}

func newZoneAwareResultTracker(instances []InstanceDesc, maxUnavailableZones int) *zoneAwareResultTracker {
	t := &zoneAwareResultTracker{
		waitingByZone:       make(map[string]int),
		failuresByZone:      make(map[string]int),
		maxUnavailableZones: maxUnavailableZones,
	}

	for _, instance := range instances {
		t.waitingByZone[instance.Zone]++
	}
	t.minSuccessfulZones = len(t.waitingByZone) - maxUnavailableZones

	return t
}

func (t *zoneAwareResultTracker) done(instance *InstanceDesc, err error) {
	t.waitingByZone[instance.Zone]--

	if err != nil {
		t.failuresByZone[instance.Zone]++
	}
}

func (t *zoneAwareResultTracker) succeeded() bool {
	successfulZones := 0

	// The execution succeeded once we successfully received a successful result
	// from "all zones - max unavailable zones".
	for zone, numWaiting := range t.waitingByZone {
		if numWaiting == 0 && t.failuresByZone[zone] == 0 {
			successfulZones++
		}
	}

	return successfulZones >= t.minSuccessfulZones
}

func (t *zoneAwareResultTracker) failed() bool {
	failedZones := len(t.failuresByZone)
	return failedZones > t.maxUnavailableZones
}
