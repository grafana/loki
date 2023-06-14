package labelvolume

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
	volumes map[string]map[string]*volume //key -> value -> [ts, val]
	limit   int32
}

type volume struct {
	Timestamp int64
	Value     uint64
}

func NewAccumulator(limit int32) *Accumulator {
	return &Accumulator{
		volumes: make(map[string]map[string]*volume),
		limit:   limit,
	}
}

// AddVolumes adds the given volumes to the accumulator at the given nanosecond timestamp.
// The limit is enforced here so that only N volumes are kept per timestamp.
func (acc *Accumulator) AddVolumes(mergedVolumes map[string]map[string]uint64, timestamp int64) {
	acc.lock.Lock()
	defer acc.lock.Unlock()

	for name, vs := range mergedVolumes {
		for value, v := range vs {
			if _, ok := acc.volumes[name]; !ok {
				acc.volumes[name] = make(map[string]*volume)
			}

			if vol, ok := acc.volumes[name][value]; !ok {
				acc.volumes[name][value] = &volume{
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
}

func (acc *Accumulator) Volumes() *logproto.LabelVolumeResponse {
	acc.lock.RLock()
	defer acc.lock.RUnlock()

	mergedVolumes := make(map[int64]map[string]map[string]uint64)
	for name, vs := range acc.volumes {
		for value, vol := range vs {
			if _, ok := mergedVolumes[vol.Timestamp]; !ok {
				mergedVolumes[vol.Timestamp] = make(map[string]map[string]uint64)
			}

			if _, ok := mergedVolumes[vol.Timestamp][name]; !ok {
				mergedVolumes[vol.Timestamp][name] = make(map[string]uint64)
			}

			if _, ok := mergedVolumes[vol.Timestamp][name][value]; !ok {
				mergedVolumes[vol.Timestamp][name][value] = vol.Value
			} else {
				mergedVolumes[vol.Timestamp][name][value] += vol.Value
			}
		}
	}

	return MapToLabelVolumeResponse(mergedVolumes, int(acc.limit))
}

func Merge(responses []*logproto.LabelVolumeResponse, limit int32) *logproto.LabelVolumeResponse {
	mergedVolumes := MergeVolumes(responses...)
	return MapToLabelVolumeResponse(mergedVolumes, int(limit))
}

func MapToLabelVolumeResponse(mergedVolumes map[int64]map[string]map[string]uint64, limit int) *logproto.LabelVolumeResponse {
	// Aggregate volumes into single value per label/value pair
	// adopting the latest timestamp seen
	acc := make(map[string]map[string]*volume)
	for timestamp, series := range mergedVolumes {
		for name, vs := range series {
			for value, v := range vs {
				if _, ok := acc[name]; !ok {
					acc[name] = make(map[string]*volume)
				}

				if vol, ok := acc[name][value]; !ok {
					acc[name][value] = &volume{
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
	}

	// Convert aggregation into slice of logproto.LabelVolume
	volumes := make([]logproto.LabelVolume, 0, len(mergedVolumes)*len(mergedVolumes[0]))
	for name, vs := range acc {
		for value, v := range vs {
			volumes = append(volumes, logproto.LabelVolume{
				Name:      name,
				Value:     value,
				Volume:    v.Value,
				Timestamp: v.Timestamp,
			})
		}
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

	return &logproto.LabelVolumeResponse{
		Volumes: volumes,
		Limit:   int32(limit),
	}
}

func MergeVolumes(responses ...*logproto.LabelVolumeResponse) map[int64]map[string]map[string]uint64 {
	mergedVolumes := make(map[int64]map[string]map[string]uint64)
	for _, res := range responses {
		if res == nil {
			// Some stores return nil responses
			continue
		}

		for _, v := range res.Volumes {
			if _, ok := mergedVolumes[v.Timestamp]; !ok {
				mergedVolumes[v.Timestamp] = make(map[string]map[string]uint64)
			}

			if _, ok := mergedVolumes[v.Timestamp][v.Name]; !ok {
				mergedVolumes[v.Timestamp][v.Name] = make(map[string]uint64)
			}
			mergedVolumes[v.Timestamp][v.Name][v.Value] += v.GetVolume()
		}
	}

	return mergedVolumes
}
