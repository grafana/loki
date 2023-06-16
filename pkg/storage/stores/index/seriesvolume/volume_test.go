package seriesvolume

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func Test_AddVolumes(t *testing.T) {
	t.Run("always accumulates values for the same label/value pair into the latest timestamp", func(t *testing.T) {
		acc := NewAccumulator(4)
		volumes := map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
			`{cluster: "dev"}`:    25,
			`{cluster: "prod"}`:   50,
		}

		acc.AddVolumes(volumes, 1)

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:      "{cluster: \"prod\"}",
					Value:     "",
					Volume:    50,
					Timestamp: 1,
				},
				{
					Name:      "{cluster: \"dev\"}",
					Value:     "",
					Volume:    25,
					Timestamp: 1,
				},
				{
					Name:      "{job: \"prometheus\"}",
					Value:     "",
					Volume:    10,
					Timestamp: 1,
				},
				{
					Name:      "{job: \"loki\"}",
					Value:     "",
					Volume:    5,
					Timestamp: 1,
				},
			},
			Limit: 4,
		}, resp)

		volumes = map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
			`{cluster: "dev"}`:    25,
			`{cluster: "prod"}`:   50,
		}

		acc.AddVolumes(volumes, 2)

		resp = acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:      "{cluster: \"prod\"}",
					Value:     "",
					Volume:    100,
					Timestamp: 2,
				},
				{
					Name:      "{cluster: \"dev\"}",
					Value:     "",
					Volume:    50,
					Timestamp: 2,
				},
				{
					Name:      "{job: \"prometheus\"}",
					Value:     "",
					Volume:    20,
					Timestamp: 2,
				},
				{
					Name:      "{job: \"loki\"}",
					Value:     "",
					Volume:    10,
					Timestamp: 2,
				},
			},
			Limit: 4,
		}, resp)
	})

	t.Run("sorts label value pairs by volume", func(t *testing.T) {
		acc := NewAccumulator(5)
		volumes := map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
			`{cluster: "dev"}`:    25,
			`{cluster: "prod"}`:   50,
		}

		acc.AddVolumes(volumes, 1)

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:      "{cluster: \"prod\"}",
					Value:     "",
					Volume:    50,
					Timestamp: 1,
				},
				{
					Name:      "{cluster: \"dev\"}",
					Value:     "",
					Volume:    25,
					Timestamp: 1,
				},
				{
					Name:      "{job: \"prometheus\"}",
					Value:     "",
					Volume:    10,
					Timestamp: 1,
				},
				{
					Name:      "{job: \"loki\"}",
					Value:     "",
					Volume:    5,
					Timestamp: 1,
				},
			},
			Limit: 5,
		}, resp)
	})

	t.Run("merges volumes for the same label value pair and timestamp", func(t *testing.T) {
		acc := NewAccumulator(5)
		volumes := map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
		}
		acc.AddVolumes(volumes, 1)

		volumes = map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
		}
		acc.AddVolumes(volumes, 1)

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:      "{job: \"prometheus\"}",
					Value:     "",
					Volume:    20,
					Timestamp: 1,
				},
				{
					Name:      "{job: \"loki\"}",
					Value:     "",
					Volume:    10,
					Timestamp: 1,
				},
			},
			Limit: 5,
		}, resp)
	})

	t.Run("aggregate volumes to latest timestamp for series and apply limit", func(t *testing.T) {
		acc := NewAccumulator(2)
		volumes := map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
			`{job: "mimir"}`:      1,
		}
		acc.AddVolumes(volumes, 1)

		volumes = map[string]uint64{
			`{job: "loki"}`:       20,
			`{job: "prometheus"}`: 30,
			`{job: "mimir"}`:      1,
		}
		acc.AddVolumes(volumes, 2)

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:      "{job: \"prometheus\"}",
					Value:     "",
					Volume:    40,
					Timestamp: 2,
				},
				{
					Name:      "{job: \"loki\"}",
					Value:     "",
					Volume:    25,
					Timestamp: 2,
				},
			},
			Limit: 2,
		}, resp)
	})
}
