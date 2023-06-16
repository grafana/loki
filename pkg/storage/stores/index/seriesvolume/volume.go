package seriesvolume

import (
	"sort"
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	MatchAny     = "{}"
	DefaultLimit = 100
)

// TODO(masslessparticle): Lock striping to reduce contention on this map
type Accumulator struct {
	lock    sync.RWMutex
	volumes map[string]*volume //key -> value -> [ts, val]
	limit   int32
}

type volume struct {
	Timestamp int64
	Value     uint64
}

func NewAccumulator(limit int32) *Accumulator {
	return &Accumulator{
		volumes: make(map[string]*volume),
		limit:   limit,
	}
}

// AddVolumes adds the given volumes to the accumulator at the given nanosecond timestamp.
// We only keep one value per label/value combination, so if a volume already exists, add to
// it and update the timestamp for the series to be the latest timestamp seen.
func (acc *Accumulator) AddVolumes(mergedVolumes map[string]uint64, timestamp int64) {
	acc.lock.Lock()
	defer acc.lock.Unlock()

	for series, v := range mergedVolumes {
		if vol, ok := acc.volumes[series]; !ok {
			acc.volumes[series] = &volume{
				Timestamp: timestamp,
				Value:     v,
			}
		} else {
			if vol.Timestamp < timestamp {
				vol.Timestamp = timestamp
			}

			vol.Value += v
		}
	}
}

func (acc *Accumulator) Volumes() *logproto.VolumeResponse {
	acc.lock.RLock()
	defer acc.lock.RUnlock()

	mergedVolumes := make(map[int64]map[string]uint64)
	for series, vol := range acc.volumes {
		if _, ok := mergedVolumes[vol.Timestamp]; !ok {
			mergedVolumes[vol.Timestamp] = make(map[string]uint64)
		}

		if _, ok := mergedVolumes[vol.Timestamp][series]; !ok {
			mergedVolumes[vol.Timestamp][series] = vol.Value
		} else {
			mergedVolumes[vol.Timestamp][series] += vol.Value
		}
	}

	return MapToSeriesVolumeResponse(mergedVolumes, int(acc.limit))
}

func Merge(responses []*logproto.VolumeResponse, limit int32) *logproto.VolumeResponse {
	mergedVolumes := MergeVolumes(responses...)
	return MapToSeriesVolumeResponse(mergedVolumes, int(limit))
}

func MapToSeriesVolumeResponse(mergedVolumes map[int64]map[string]uint64, limit int) *logproto.VolumeResponse {
	// Aggregate volumes into single value per label/value pair
	// adopting the latest timestamp seen
	acc := make(map[string]*volume)
	for timestamp, series := range mergedVolumes {
		for name, v := range series {
			if vol, ok := acc[name]; !ok {
				acc[name] = &volume{
					Timestamp: timestamp,
					Value:     v,
				}
			} else {
				if vol.Timestamp < timestamp {
					vol.Timestamp = timestamp
				}

				vol.Value += v
			}
		}
	}

	// Convert aggregation into slice of logproto.Volume
	volumes := make([]logproto.Volume, 0, len(mergedVolumes)*len(mergedVolumes[0]))
	for name, v := range acc {
		volumes = append(volumes, logproto.Volume{
			Name:      name,
			Value:     "",
			Volume:    v.Value,
			Timestamp: v.Timestamp,
		})
	}

	// Sort the LabelVolume response by timestamp, volume, name, value
	// to ensure consistency in responses
	sort.Slice(volumes, func(i, j int) bool {
		if volumes[i].Timestamp == volumes[j].Timestamp {
			if volumes[i].Volume == volumes[j].Volume {
				if volumes[i].Name == volumes[j].Name {
					return volumes[i].Value < volumes[j].Value
				}

				return volumes[i].Name < volumes[j].Name
			}

			return volumes[i].Volume > volumes[j].Volume
		}
		return volumes[i].Timestamp < volumes[j].Timestamp
	})

	// Apply the limit
	if len(volumes) > limit {
		volumes = volumes[:limit]
	}

	return &logproto.VolumeResponse{
		Volumes: volumes,
		Limit:   int32(limit),
	}
}

func MergeVolumes(responses ...*logproto.VolumeResponse) map[int64]map[string]uint64 {
	mergedVolumes := make(map[int64]map[string]uint64)
	for _, res := range responses {
		if res == nil {
			// Some stores return nil responses
			continue
		}

		for _, v := range res.Volumes {
			if _, ok := mergedVolumes[v.Timestamp]; !ok {
				mergedVolumes[v.Timestamp] = make(map[string]uint64)
			}

			if _, ok := mergedVolumes[v.Timestamp][v.Name]; !ok {
				mergedVolumes[v.Timestamp][v.Name] = v.GetVolume()
			} else {
				mergedVolumes[v.Timestamp][v.Name] += v.GetVolume()
			}
		}
	}

	return mergedVolumes
}
