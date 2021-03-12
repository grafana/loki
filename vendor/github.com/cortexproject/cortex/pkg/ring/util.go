package ring

import (
	"context"
	"math/rand"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
)

// GenerateTokens make numTokens unique random tokens, none of which clash
// with takenTokens. Generated tokens are sorted.
func GenerateTokens(numTokens int, takenTokens []uint32) []uint32 {
	if numTokens <= 0 {
		return []uint32{}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	used := make(map[uint32]bool, len(takenTokens))
	for _, v := range takenTokens {
		used[v] = true
	}

	tokens := make([]uint32, 0, numTokens)
	for i := 0; i < numTokens; {
		candidate := r.Uint32()
		if used[candidate] {
			continue
		}
		used[candidate] = true
		tokens = append(tokens, candidate)
		i++
	}

	// Ensure returned tokens are sorted.
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i] < tokens[j]
	})

	return tokens
}

// GetInstanceAddr returns the address to use to register the instance
// in the ring.
func GetInstanceAddr(configAddr string, netInterfaces []string) (string, error) {
	if configAddr != "" {
		return configAddr, nil
	}

	addr, err := util.GetFirstAddressOf(netInterfaces)
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
func WaitInstanceState(ctx context.Context, r *Ring, instanceID string, state InstanceState) error {
	backoff := util.NewBackoff(ctx, util.BackoffConfig{
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
func WaitRingStability(ctx context.Context, r *Ring, op Operation, minStability, maxWaiting time.Duration) error {
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

			if HasReplicationSetChanged(ringLastState, currRingState) {
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
	i := sort.Search(len(tokens), func(x int) bool {
		return tokens[x] > key
	})
	if i >= len(tokens) {
		i = 0
	}
	return i
}
