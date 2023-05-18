package labelvolume

import (
	"github.com/grafana/loki/pkg/logproto"
	"sort"
)

const MatchAny = "{}"

func Merge(responses []*logproto.LabelVolumeResponse) *logproto.LabelVolumeResponse {
	mergedVolumes := make(map[string]map[string]uint64)
	for _, res := range responses {
		for _, v := range res.Volumes {
			if _, ok := mergedVolumes[v.Name]; !ok {
				mergedVolumes[v.Name] = make(map[string]uint64)
			}
			mergedVolumes[v.Name][v.Value] += v.GetVolume()
		}
	}

	return MapToLabelVolumeResponse(mergedVolumes)
}

func MapToLabelVolumeResponse(mergedVolumes map[string]map[string]uint64) *logproto.LabelVolumeResponse {
	volumes := make([]logproto.LabelVolume, 0, len(mergedVolumes))
	for name, v := range mergedVolumes {
		for value, volume := range v {
			volumes = append(volumes, logproto.LabelVolume{
				Name:   name,
				Value:  value,
				Volume: volume,
			})
		}
	}

	sort.Slice(volumes, func(i, j int) bool {
		if volumes[i].Name == volumes[j].Name {
			return volumes[i].Value < volumes[j].Value
		}

		return volumes[i].Name < volumes[j].Name
	})

	return &logproto.LabelVolumeResponse{Volumes: volumes}
}
