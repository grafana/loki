package labelvolume

import (
	"github.com/grafana/loki/pkg/logproto"
	"sort"
)

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
		return volumes[i].Name < volumes[j].Name
	})

	return &logproto.LabelVolumeResponse{Volumes: volumes}
}
