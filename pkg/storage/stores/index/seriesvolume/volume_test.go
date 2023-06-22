package seriesvolume

import (
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/grafana/loki/pkg/logproto"
)

func Test_AddVolumes(t *testing.T) {
	volumes := map[string]uint64{
		`{job: "loki"}`:       5,
		`{job: "prometheus"}`: 10,
		`{cluster: "dev"}`:    25,
		`{cluster: "prod"}`:   50,
	}

	t.Run("accumulates values for the same series", func(t *testing.T) {
		acc := NewAccumulator(4)
		acc.AddVolumes(volumes)

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{cluster: \"prod\"}",
					Volume: 50,
				},
				{
					Name:   "{cluster: \"dev\"}",
					Volume: 25,
				},
				{
					Name:   "{job: \"prometheus\"}",
					Volume: 10,
				},
				{
					Name:   "{job: \"loki\"}",
					Volume: 5,
				},
			},
			Limit: 4,
		}, resp)

		acc.AddVolumes(volumes)

		resp = acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{cluster: \"prod\"}",
					Volume: 100,
				},
				{
					Name:   "{cluster: \"dev\"}",
					Volume: 50,
				},
				{
					Name:   "{job: \"prometheus\"}",
					Volume: 20,
				},
				{
					Name:   "{job: \"loki\"}",
					Volume: 10,
				},
			},
			Limit: 4,
		}, resp)
	})

	t.Run("sorts label value pairs by volume", func(t *testing.T) {
		acc := NewAccumulator(5)
		acc.AddVolumes(volumes)

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{cluster: \"prod\"}",
					Volume: 50,
				},
				{
					Name:   "{cluster: \"dev\"}",
					Volume: 25,
				},
				{
					Name:   "{job: \"prometheus\"}",
					Volume: 10,
				},
				{
					Name:   "{job: \"loki\"}",
					Volume: 5,
				},
			},
			Limit: 5,
		}, resp)
	})

	t.Run("applies limit", func(t *testing.T) {
		acc := NewAccumulator(2)
		volumes := map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
			`{job: "mimir"}`:      1,
		}
		acc.AddVolumes(volumes)

		volumes = map[string]uint64{
			`{job: "loki"}`:       20,
			`{job: "prometheus"}`: 30,
			`{job: "mimir"}`:      1,
		}
		acc.AddVolumes(volumes)

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{job: \"prometheus\"}",
					Volume: 40,
				},
				{
					Name:   "{job: \"loki\"}",
					Volume: 25,
				},
			},
			Limit: 2,
		}, resp)
	})
}

func Test_Merge(t *testing.T) {
	t.Run("merges and sorts multiple volume responses into a single response with values aggregated", func(t *testing.T) {
		limit := int32(5)
		responses := []*logproto.VolumeResponse{
			{
				Volumes: []logproto.Volume{
					{
						Name:   "{cluster: \"dev\"}",
						Volume: 25,
					},
					{
						Name:   "{cluster: \"prod\"}",
						Volume: 50,
					},
				},
				Limit: limit,
			},
			{
				Volumes: []logproto.Volume{
					{
						Name:   "{cluster: \"dev\"}",
						Volume: 25,
					},
					{
						Name:   "{job: \"foo\"}",
						Volume: 15,
					},
					{
						Name:   "{cluster: \"prod\"}",
						Volume: 50,
					},
				},
				Limit: limit,
			},
		}

		mergedResponse := Merge(responses, limit)

		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{cluster: \"prod\"}",
					Volume: 100,
				},
				{
					Name:   "{cluster: \"dev\"}",
					Volume: 50,
				},
				{
					Name:   "{job: \"foo\"}",
					Volume: 15,
				},
			},
			Limit: limit,
		}, mergedResponse)
	})

	t.Run("applies limit to return N biggest series", func(t *testing.T) {
		limit := int32(2)
		responses := []*logproto.VolumeResponse{
			{
				Volumes: []logproto.Volume{
					{
						Name:   "{cluster: \"dev\"}",
						Volume: 25,
					},
					{
						Name:   "{cluster: \"prod\"}",
						Volume: 50,
					},
				},
				Limit: limit,
			},
			{
				Volumes: []logproto.Volume{
					{
						Name:   "{cluster: \"dev\"}",
						Volume: 25,
					},
					{
						Name:   "{job: \"foo\"}",
						Volume: 15,
					},
					{
						Name:   "{cluster: \"prod\"}",
						Volume: 50,
					},
				},
				Limit: limit,
			},
		}

		mergedResponse := Merge(responses, limit)

		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{cluster: \"prod\"}",
					Volume: 100,
				},
				{
					Name:   "{cluster: \"dev\"}",
					Volume: 50,
				},
			},
			Limit: limit,
		}, mergedResponse)
	})

	t.Run("aggregates responses into earliest from and latest through timestamp of input", func(t *testing.T) {
		limit := int32(5)
		responses := []*logproto.VolumeResponse{
			{
				Volumes: []logproto.Volume{
					{
						Name:   "{cluster: \"dev\"}",
						Volume: 25,
					},
					{
						Name:   "{cluster: \"prod\"}",
						Volume: 50,
					},
				},
				Limit: limit,
			},
			{
				Volumes: []logproto.Volume{
					{
						Name:   "{cluster: \"dev\"}",
						Volume: 25,
					},
					{
						Name:   "{job: \"foo\"}",
						Volume: 15,
					},
					{
						Name:   "{cluster: \"prod\"}",
						Volume: 50,
					},
				},
				Limit: limit,
			},
		}

		mergedResponse := Merge(responses, limit)

		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{cluster: \"prod\"}",
					Volume: 100,
				},
				{
					Name:   "{cluster: \"dev\"}",
					Volume: 50,
				},
				{
					Name:   "{job: \"foo\"}",
					Volume: 15,
				},
			},
			Limit: limit,
		}, mergedResponse)
	})
}
