package xcap

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/grafana/loki/v3/pkg/xcap/statid"
)

var (
	statisticsByID [statid.Count]Statistic
	statisticsMu   sync.RWMutex

	unknownStatisticObservations atomic.Uint64
)

// registerStatistic adds a statistic to the process-wide registry.
// Statistics without an ID are local to the process and cannot be
// encoded on the wire.
func registerStatistic(stat Statistic) {
	id := stat.ID()
	if id == statid.Invalid {
		return
	}
	if id >= statid.Count {
		panic(fmt.Sprintf("xcap: statistic %q has invalid ID %d", stat.Name(), id))
	}
	statisticsMu.Lock()
	defer statisticsMu.Unlock()

	if statid.IsReserved(id) {
		panic(fmt.Sprintf("xcap: statistic %q uses reserved ID %d", stat.Name(), id))
	}

	existing := statisticsByID[id]
	if existing == nil {
		statisticsByID[id] = stat
		return
	}
	if existing.Key() != stat.Key() || existing.Scope() != stat.Scope() {
		panic(fmt.Sprintf("xcap: statistic ID %d registered for both %v and %v", id, existing.Key(), stat.Key()))
	}
}

func statisticByID(id uint32) (Statistic, bool) {
	if id == uint32(statid.Invalid) || id >= uint32(statid.Count) {
		return nil, false
	}

	statisticsMu.RLock()
	if statid.IsReserved(statid.ID(id)) {
		statisticsMu.RUnlock()
		return nil, false
	}
	stat := statisticsByID[id]
	statisticsMu.RUnlock()
	return stat, stat != nil
}

// ValidateStatisticRegistry reports any stat IDs declared in [statid] that
// were not registered by the linked statistic-defining packages. Applications
// can call it during startup to detect missing registrations before accepting
// captures.
func ValidateStatisticRegistry() error {
	statisticsMu.RLock()
	defer statisticsMu.RUnlock()

	return validateStatisticRegistryWithReserved(statisticsByID, func(id statid.ID) bool {
		return statid.IsReserved(id)
	})
}

// MustValidateStatisticRegistry panics when a declared statistic ID was not
// registered. Applications can call it during startup to make a linkage or
// registration error fail fast.
func MustValidateStatisticRegistry() {
	if err := ValidateStatisticRegistry(); err != nil {
		panic(err)
	}
}

func validateStatisticRegistry(statistics [statid.Count]Statistic) error {
	return validateStatisticRegistryWithReserved(statistics, statid.IsReserved)
}

func validateStatisticRegistryWithReserved(statistics [statid.Count]Statistic, isReserved func(statid.ID) bool) error {
	for id := statid.ID(1); id < statid.Count; id++ {
		if isReserved(id) {
			continue
		}
		if statistics[id] == nil {
			return fmt.Errorf("xcap: statistic ID %d is not registered", id)
		}
	}
	return nil
}

func recordUnknownStatisticObservation() {
	unknownStatisticObservations.Add(1)
}

// UnknownStatisticObservations returns the number of wire observations skipped
// because their statistic ID is not registered in this process.
func UnknownStatisticObservations() uint64 {
	return unknownStatisticObservations.Load()
}
