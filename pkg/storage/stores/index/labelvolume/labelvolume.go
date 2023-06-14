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
	volumes map[int64]map[string]map[string]uint64
	limit   int32
}

func NewAccumulator(limit int32) *Accumulator {
	return &Accumulator{
		volumes: make(map[int64]map[string]map[string]uint64),
		limit:   limit,
	}
}

// AddVolumes adds the given volumes to the accumulator at the given nanosecond timestamp.
// The limit is enforced here so that only N volumes are kept per timestamp.
func (acc *Accumulator) AddVolumes(v map[string]map[string]uint64, timestamp int64) {
	acc.lock.Lock()
	defer acc.lock.Unlock()

  // TODO(trevorwhitney): this new data structure is no longer needed once we flatten the
  // input to a map of selectors to volumes.
	volumes := make([]logproto.LabelVolume, 0, len(v))
	for label, vs := range v {
		for value, volume := range vs {
			volumes = append(volumes, logproto.LabelVolume{
				Name:      label,
				Value:     value,
				Volume:    volume,
				Timestamp: timestamp,
			})
		}
	}

	sort.Slice(volumes, func(i, j int) bool {
		if volumes[i].Volume == volumes[j].Volume {
			if volumes[i].Name == volumes[j].Name {
				return volumes[i].Value < volumes[j].Value
			}

			return volumes[i].Name < volumes[j].Name
		}

		return volumes[i].Volume > volumes[j].Volume
	})

	if acc.limit < int32(len(volumes)) {
		volumes = volumes[:acc.limit]
	}

	for _, v := range volumes {
		if _, ok := acc.volumes[v.Timestamp]; !ok {
			acc.volumes[v.Timestamp] = make(map[string]map[string]uint64)
		}

		if _, ok := acc.volumes[v.Timestamp][v.Name]; !ok {
			acc.volumes[v.Timestamp][v.Name] = make(map[string]uint64)
		}

		if _, ok := acc.volumes[v.Timestamp][v.Name][v.Value]; !ok {
			acc.volumes[v.Timestamp][v.Name][v.Value] = 0
		}

		acc.volumes[v.Timestamp][v.Name][v.Value] += v.Volume
	}
}

func (acc *Accumulator) Volumes() *logproto.LabelVolumeResponse {
	acc.lock.RLock()
	defer acc.lock.RUnlock()

	return MapToLabelVolumeResponse(acc.volumes, int(acc.limit))
}

func Merge(responses []*logproto.LabelVolumeResponse, limit int32) *logproto.LabelVolumeResponse {
	mergedVolumes := MergeVolumes(responses...)
	return MapToLabelVolumeResponse(mergedVolumes, int(limit))
}

func MapToLabelVolumeResponse(mergedVolumes map[int64]map[string]map[string]uint64, limit int) *logproto.LabelVolumeResponse {
	volumes := make([]logproto.LabelVolume, 0, len(mergedVolumes)*len(mergedVolumes[0]))
	for ts, vs := range mergedVolumes {
		for name, v := range vs {
			for value, volume := range v {
				volumes = append(volumes, logproto.LabelVolume{
					Name:      name,
					Value:     value,
					Volume:    volume,
					Timestamp: ts,
				})
			}
		}
	}

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
