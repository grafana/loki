package labelvolume

import (
	"testing"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func Test_AddVolumes(t *testing.T) {
	t.Run("sorts label value pairs by volume", func(t *testing.T) {
		acc := NewAccumulator(5)
		volumes := map[string]map[string]uint64{
			"job": {
				"loki":       5,
				"prometheus": 10,
			},
			"cluster": {
				"dev":  25,
				"prod": 50,
			},
		}

		acc.AddVolumes(volumes, 1)

		resp := acc.Volumes()
		require.Equal(t, &logproto.LabelVolumeResponse{
			Volumes: []logproto.LabelVolume{
				{
					Name:      "cluster",
					Value:     "prod",
					Volume:    50,
					Timestamp: 1,
				},
				{
					Name:      "cluster",
					Value:     "dev",
					Volume:    25,
					Timestamp: 1,
				},
				{
					Name:      "job",
					Value:     "prometheus",
					Volume:    10,
					Timestamp: 1,
				},
				{
					Name:      "job",
					Value:     "loki",
					Volume:    5,
					Timestamp: 1,
				},
			},
			Limit: 5,
		}, resp)
	})

	t.Run("merges volumes for the same label value pair and timestamp", func(t *testing.T) {
		acc := NewAccumulator(5)
		volumes := map[string]map[string]uint64{
			"job": {
				"loki":       5,
				"prometheus": 10,
			},
		}
		acc.AddVolumes(volumes, 1)

		volumes = map[string]map[string]uint64{
			"job": {
				"loki":       5,
				"prometheus": 10,
			},
		}
		acc.AddVolumes(volumes, 1)

		resp := acc.Volumes()
		require.Equal(t, &logproto.LabelVolumeResponse{
			Volumes: []logproto.LabelVolume{
				{
					Name:      "job",
					Value:     "prometheus",
					Volume:    20,
					Timestamp: 1,
				},
				{
					Name:      "job",
					Value:     "loki",
					Volume:    10,
					Timestamp: 1,
				},
			},
			Limit: 5,
		}, resp)
	})

	t.Run("only accumulate volumes for the same timstamp and sorts by timestamp before volume", func(t *testing.T) {
		acc := NewAccumulator(5)
		volumes := map[string]map[string]uint64{
			"job": {
				"loki":       5,
				"prometheus": 10,
			},
		}
		acc.AddVolumes(volumes, 1)

		volumes = map[string]map[string]uint64{
			"job": {
				"loki":       20,
				"prometheus": 30,
			},
		}
		acc.AddVolumes(volumes, 2)

		resp := acc.Volumes()
		require.Equal(t, &logproto.LabelVolumeResponse{
			Volumes: []logproto.LabelVolume{
				{
					Name:      "job",
					Value:     "prometheus",
					Volume:    10,
					Timestamp: 1,
				},
				{
					Name:      "job",
					Value:     "loki",
					Volume:    5,
					Timestamp: 1,
				},
				{
					Name:      "job",
					Value:     "prometheus",
					Volume:    30,
					Timestamp: 2,
				},
				{
					Name:      "job",
					Value:     "loki",
					Volume:    20,
					Timestamp: 2,
				},
			},
			Limit: 5,
		}, resp)
	})
}
