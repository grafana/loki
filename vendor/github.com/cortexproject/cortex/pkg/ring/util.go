package ring

import (
	"context"
	"math/rand"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
)

// GenerateTokens make numTokens unique random tokens, none of which clash
// with takenTokens.
func GenerateTokens(numTokens int, takenTokens []uint32) []uint32 {
	if numTokens <= 0 {
		return []uint32{}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	used := make(map[uint32]bool, len(takenTokens))
	for _, v := range takenTokens {
		used[v] = true
	}

	tokens := []uint32{}
	for i := 0; i < numTokens; {
		candidate := r.Uint32()
		if used[candidate] {
			continue
		}
		used[candidate] = true
		tokens = append(tokens, candidate)
		i++
	}

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
func WaitInstanceState(ctx context.Context, r *Ring, instanceID string, state IngesterState) error {
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

// getZones return the list zones from the provided tokens. The returned list
// is guaranteed to be sorted.
func getZones(tokens map[string][]TokenDesc) []string {
	var zones []string

	for zone := range tokens {
		zones = append(zones, zone)
	}

	sort.Strings(zones)
	return zones
}

// searchToken returns the offset of the tokens entry holding the range for the provided key.
func searchToken(tokens []TokenDesc, key uint32) int {
	i := sort.Search(len(tokens), func(x int) bool {
		return tokens[x].Token > key
	})
	if i >= len(tokens) {
		i = 0
	}
	return i
}
