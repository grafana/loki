package usage

import "time"

const (
	initialEntriesCapacity = 1000
)

type usageEntry struct {
	bytes     uint64
	timestamp time.Time
}

type streamStats struct {
	entries []usageEntry
}

type tenantStats struct {
	streams map[uint64]*streamStats
}

type partitionStats struct {
	tenants   map[string]*tenantStats
	offset    int64
	timestamp time.Time
}

type usageStats struct {
	stats map[int32]*partitionStats
}

func newUsageStats() *usageStats {
	return &usageStats{
		stats: make(map[int32]*partitionStats),
	}
}

func (s *streamStats) totalBytesSince(since time.Time) uint64 {
	var total uint64
	for _, entry := range s.entries {
		if entry.timestamp.After(since) {
			total += entry.bytes
		}
	}
	return total
}

func (u *usageStats) addEntry(partition int32, tenantID string, streamHash uint64, bytes uint64, timestamp time.Time, offset int64) {
	// Get or create partition stats
	pStats, ok := u.stats[partition]
	if !ok {
		pStats = &partitionStats{
			tenants: make(map[string]*tenantStats),
		}
		u.stats[partition] = pStats
	}

	pStats.offset = offset
	pStats.timestamp = timestamp

	// Get or create tenant stats
	tStats, ok := pStats.tenants[tenantID]
	if !ok {
		tStats = &tenantStats{
			streams: make(map[uint64]*streamStats),
		}
		pStats.tenants[tenantID] = tStats
	}

	// Get or create stream stats
	sStats, ok := tStats.streams[streamHash]
	if !ok {
		sStats = &streamStats{
			entries: make([]usageEntry, 0, initialEntriesCapacity),
		}
		tStats.streams[streamHash] = sStats
	}

	// Simply append the new entry
	sStats.entries = append(sStats.entries, usageEntry{
		bytes:     bytes,
		timestamp: timestamp,
	})
}

func (u *usageStats) evictBefore(cutoff time.Time) {
	for partition, pStats := range u.stats {
		for tenantID, tStats := range pStats.tenants {
			for streamKey, sStats := range tStats.streams {
				// Filter entries in place
				n := 0
				for _, entry := range sStats.entries {
					if entry.timestamp.After(cutoff) {
						sStats.entries[n] = entry
						n++
					}
				}

				if n == 0 {
					// All entries are old, remove the stream
					delete(tStats.streams, streamKey)
				} else {
					// Reslice to keep only newer entries
					sStats.entries = sStats.entries[:n]
				}
			}
			// Clean up empty tenant entries
			if len(tStats.streams) == 0 {
				delete(pStats.tenants, tenantID)
			}
		}
		// Clean up empty partition entries
		if len(pStats.tenants) == 0 {
			delete(u.stats, partition)
		}
	}
}
