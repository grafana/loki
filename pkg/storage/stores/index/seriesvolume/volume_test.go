package seriesvolume

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_AddVolume(t *testing.T) {
	volumes := map[string]uint64{
		`{job: "loki"}`:       5,
		`{job: "prometheus"}`: 10,
		`{cluster: "dev"}`:    25,
		`{cluster: "prod"}`:   50,
	}

	t.Run("accumulates values for the same series", func(t *testing.T) {
		acc := NewAccumulator(4, 10)
		for name, size := range volumes {
			_ = acc.AddVolume(name, size)
		}

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

		for name, size := range volumes {
			_ = acc.AddVolume(name, size)
		}
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
		acc := NewAccumulator(5, 10)
		for name, size := range volumes {
			_ = acc.AddVolume(name, size)
		}
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
		acc := NewAccumulator(2, 10)
		volumes := map[string]uint64{
			`{job: "loki"}`:       5,
			`{job: "prometheus"}`: 10,
			`{job: "mimir"}`:      1,
		}
		for name, size := range volumes {
			_ = acc.AddVolume(name, size)
		}

		resp := acc.Volumes()
		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   "{job: \"prometheus\"}",
					Volume: 10,
				},
				{
					Name:   "{job: \"loki\"}",
					Volume: 5,
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

	t.Run("it returns an error when there are more than maxSize volumes", func(t *testing.T) {
		acc := NewAccumulator(2, 0)

		err := acc.AddVolume(`{job: "loki"}`, 5)
		require.EqualError(t, err, fmt.Sprintf(ErrVolumeMaxSeriesHit, 0))
	})
}
