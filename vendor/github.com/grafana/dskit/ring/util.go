package ring

import (
	"context"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/netutil"
)

// GetInstanceAddr returns the address to use to register the instance
// in the ring.
func GetInstanceAddr(configAddr string, netInterfaces []string, logger log.Logger, enableInet6 bool) (string, error) {
	if configAddr != "" {
		return configAddr, nil
	}

	addr, err := netutil.GetFirstAddressOf(netInterfaces, logger, enableInet6)
	if err != nil {
		return "", err
	}

	return addr, nil
}

// GetInstancePort returns the port to use to register the instance
// in the ring.
func GetInstancePort(configPort, listenPort int) int {
	if configPort > 0 {
		return configPort
	}

	return listenPort
}

// WaitInstanceState waits until the input instanceID is registered within the
// ring matching the provided state. A timeout should be provided within the context.
func WaitInstanceState(ctx context.Context, r ReadRing, instanceID string, state InstanceState) error {
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: time.Second,
		MaxRetries: 0,
	})

	for backoff.Ongoing() {
		if actualState, err := r.GetInstanceState(instanceID); err == nil && actualState == state {
			return nil
		}

		backoff.Wait()
	}

	return backoff.Err()
}

// WaitRingStability monitors the ring topology for the provided operation and waits until it
// keeps stable for at least minStability.
func WaitRingStability(ctx context.Context, r ReadRing, op Operation, minStability, maxWaiting time.Duration) error {
	return waitStability(ctx, r, op, minStability, maxWaiting, HasReplicationSetChanged)
}

// WaitRingTokensStability waits for the Ring to be unchanged at
// least for minStability time period, excluding transitioning between
// allowed states (e.g. JOINING->ACTIVE if allowed by op).
// This can be used to avoid wasting resources on moving data around
// due to multiple changes in the Ring.
func WaitRingTokensStability(ctx context.Context, r ReadRing, op Operation, minStability, maxWaiting time.Duration) error {
	return waitStability(ctx, r, op, minStability, maxWaiting, HasReplicationSetChangedWithoutState)
}

func waitStability(ctx context.Context, r ReadRing, op Operation, minStability, maxWaiting time.Duration, isChanged func(ReplicationSet, ReplicationSet) bool) error {
	// Configure the max waiting time as a context deadline.
	ctx, cancel := context.WithTimeout(ctx, maxWaiting)
	defer cancel()

	// Get the initial ring state.
	ringLastState, _ := r.GetAllHealthy(op) // nolint:errcheck
	ringLastStateTs := time.Now()

	const pollingFrequency = time.Second
	pollingTicker := time.NewTicker(pollingFrequency)
	defer pollingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pollingTicker.C:
			// We ignore the error because in case of error it will return an empty
			// replication set which we use to compare with the previous state.
			currRingState, _ := r.GetAllHealthy(op) // nolint:errcheck

			if isChanged(ringLastState, currRingState) {
				ringLastState = currRingState
				ringLastStateTs = time.Now()
			} else if time.Since(ringLastStateTs) >= minStability {
				return nil
			}
		}
	}
}

// MakeBuffersForGet returns buffers to use with Ring.Get().
func MakeBuffersForGet() (bufDescs []InstanceDesc, bufHosts, bufZones []string) {
	bufDescs = make([]InstanceDesc, 0, GetBufferSize)
	bufHosts = make([]string, 0, GetBufferSize)
	bufZones = make([]string, 0, GetBufferSize)
	return
}

// getZones return the list zones from the provided tokens. The returned list
// is guaranteed to be sorted.
func getZones(tokens map[string][]uint32) []string {
	var zones []string

	for zone := range tokens {
		zones = append(zones, zone)
	}

	sort.Strings(zones)
	return zones
}

// searchToken returns the offset of the tokens entry holding the range for the provided key.
func searchToken(tokens []uint32, key uint32) int {
	i, found := slices.BinarySearch(tokens, key)
	if found {
		// we want the first token > key, not >= key
		i = i + 1
	}
	if i >= len(tokens) {
		i = 0
	}
	return i
}

// tokenDistance returns the distance between the given tokens from and to.
// The distance between a token and itself is the whole ring, i.e., math.MaxUint32 + 1.
func tokenDistance(from, to uint32) int64 {
	if from < to {
		return int64(to - from)
	}
	// the trailing +1 is needed to ensure that token 0 is counted
	return math.MaxUint32 - int64(from) + int64(to) + 1
}

// stringSet is a set of strings optimized for both small and large sizes.
// It uses a slice for small sets (zero allocations when using a pre-allocated buffer)
// and automatically switches to a map when the slice is full.
//
// The rationale is that looking for a string in a small slice is faster than in a map: the
// map implementation becomes faster – compared to a slice – the more the slice
// gets bigger, so we switch to a map only when the slice is full.
type stringSet struct {
	setSlice []string
	setMap   map[string]struct{}
	count    int
}

func newStringSet(sliceBuf []string) *stringSet {
	return &stringSet{
		setSlice: sliceBuf[:0],
		setMap:   nil,
	}
}

// contains returns true if the set contains the given string.
func (s *stringSet) contains(str string) bool {
	if s.setMap != nil {
		_, ok := s.setMap[str]
		return ok
	}
	return slices.Contains(s.setSlice, str)
}

// add adds a string to the set. This function does NOT check if the string is already present:
// the caller MUST ensure that by calling contains() before add().
func (s *stringSet) add(str string) {
	s.count++

	if s.setMap != nil {
		s.setMap[str] = struct{}{}
	} else if len(s.setSlice) < cap(s.setSlice) {
		s.setSlice = append(s.setSlice, str)
	} else {
		// Slice is full, switch to map.
		s.setMap = make(map[string]struct{}, len(s.setSlice)*2)

		// Copy the data from the slice to the map.
		for _, h := range s.setSlice {
			s.setMap[h] = struct{}{}
		}

		// Add the new string to the map.
		s.setMap[str] = struct{}{}
	}
}

// len returns the number of strings in the set.
func (s *stringSet) len() int {
	return s.count
}
