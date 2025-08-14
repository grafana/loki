package executor

import (
	"iter"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func Test_streamsView(t *testing.T) {
	inputStreams := []labels.Labels{
		labels.FromStrings("app", "loki", "env", "prod", "region", "us-west"),
		labels.FromStrings("app", "loki", "env", "dev"),
		labels.FromStrings("app", "loki", "env", "prod", "region", "us-east"),
	}

	sec := buildStreamsSection(t, inputStreams)

	t.Run("default", func(t *testing.T) {
		view := newStreamsView(sec, &streamsViewOptions{
			BatchSize: 1,
		})

		var actual []labels.Labels

		for id := 1; id <= 3; id++ {
			it, err := view.Labels(t.Context(), int64(id))
			require.NoError(t, err, "failed to get labels iterator")
			actual = append(actual, collectLabels(it))
		}

		require.Equal(t, inputStreams, actual, "expected all streams to be returned")
	})

	t.Run("one stream", func(t *testing.T) {
		view := newStreamsView(sec, &streamsViewOptions{
			StreamIDs: []int64{2},
			BatchSize: 1,
		})

		var actual []labels.Labels

		it, err := view.Labels(t.Context(), int64(2))
		require.NoError(t, err, "failed to get labels iterator")
		actual = append(actual, collectLabels(it))

		expected := []labels.Labels{
			inputStreams[1], // Stream ID 2
		}
		require.Equal(t, expected, actual, "expected only specified streams to be returned")
	})

	t.Run("specific streams", func(t *testing.T) {
		view := newStreamsView(sec, &streamsViewOptions{
			StreamIDs: []int64{2, 3},
			BatchSize: 1,
		})

		var actual []labels.Labels

		for _, id := range []int{2, 3} {
			it, err := view.Labels(t.Context(), int64(id))
			require.NoError(t, err, "failed to get labels iterator")
			actual = append(actual, collectLabels(it))
		}

		expected := []labels.Labels{
			inputStreams[1], // Stream ID 2
			inputStreams[2], // Stream ID 3
		}
		require.Equal(t, expected, actual, "expected only specified streams to be returned")
	})

	t.Run("one label", func(t *testing.T) {
		regionColumnIndex := slices.IndexFunc(sec.Columns(), func(c *streams.Column) bool {
			return c.Name == "region"
		})

		view := newStreamsView(sec, &streamsViewOptions{
			LabelColumns: []*streams.Column{sec.Columns()[regionColumnIndex]},
			BatchSize:    1,
		})

		expect := []labels.Labels{
			labels.FromStrings("region", "us-west"),
			labels.FromStrings(),
			labels.FromStrings("region", "us-east"),
		}

		var actual []labels.Labels

		for id := 1; id <= 3; id++ {
			it, err := view.Labels(t.Context(), int64(id))
			require.NoError(t, err, "failed to get labels iterator")
			actual = append(actual, collectLabels(it))
		}

		require.Equal(t, expect, actual, "expected all streams to be returned with the proper labels")
	})

	t.Run("two labels", func(t *testing.T) {
		appColumnIndex := slices.IndexFunc(sec.Columns(), func(c *streams.Column) bool {
			return c.Name == "app"
		})
		envColumnIndex := slices.IndexFunc(sec.Columns(), func(c *streams.Column) bool {
			return c.Name == "env"
		})

		view := newStreamsView(sec, &streamsViewOptions{
			LabelColumns: []*streams.Column{
				sec.Columns()[appColumnIndex],
				sec.Columns()[envColumnIndex],
			},
			BatchSize: 1,
		})

		expect := []labels.Labels{
			labels.FromStrings("app", "loki", "env", "prod"),
			labels.FromStrings("app", "loki", "env", "dev"),
			labels.FromStrings("app", "loki", "env", "prod"),
		}

		var actual []labels.Labels

		for id := 1; id <= 3; id++ {
			it, err := view.Labels(t.Context(), int64(id))
			require.NoError(t, err, "failed to get labels iterator")
			actual = append(actual, collectLabels(it))
		}

		require.Equal(t, expect, actual, "expected all streams to be returned with the proper labels")
	})
}

func buildStreamsSection(t *testing.T, streamLabels []labels.Labels) *streams.Section {
	streamsBuilder := streams.NewBuilder(nil, 8192)
	for _, stream := range streamLabels {
		_ = streamsBuilder.Record(stream, time.Now().UTC(), 0)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(streamsBuilder), "failed to append streams section")

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err, "failed to flush dataobj")
	t.Cleanup(func() { closer.Close() })

	sec, err := streams.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err, "failed to open streams section")
	return sec
}

func collectLabels(it iter.Seq[labels.Label]) labels.Labels {
	var ls []labels.Label
	for l := range it {
		ls = append(ls, l)
	}
	return labels.New(ls...)
}
