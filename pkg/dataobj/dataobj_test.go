package dataobj

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func Test(t *testing.T) {
	bucket := objstore.NewInMemBucket()

	streams := []logproto.Stream{
		{
			Labels: `{cluster="test",app="foo"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(10, 0).UTC(),
					Line:      "hello",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "123"},
					},
				},
				{
					Timestamp: time.Unix(5, 0).UTC(),
					Line:      "hello again",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "456"},
						{Name: "span_id", Value: "789"},
					},
				},
			},
		},

		{
			Labels: `{cluster="test",app="bar"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(15, 0).UTC(),
					Line:      "world",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "abc"},
					},
				},
				{
					Timestamp: time.Unix(20, 0).UTC(),
					Line:      "world again",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "def"},
						{Name: "span_id", Value: "ghi"},
					},
				},
			},
		},
	}

	t.Run("Build", func(t *testing.T) {
		// Create a tiny builder which flushes a lot of objects and pages to properly
		// test the builder.
		builderConfig := BuilderConfig{
			SHAPrefixSize: 2,

			TargetPageSize:   1_500_000,
			TargetObjectSize: 10_000_000,
		}

		builder, err := NewBuilder(builderConfig, bucket, "fake")
		require.NoError(t, err)

		for _, entry := range streams {
			require.NoError(t, builder.Append(entry))
		}
		require.NoError(t, builder.Flush(context.Background()))
	})

	t.Run("Read", func(t *testing.T) {
		reader := newReader(bucket)

		objects, err := result.Collect(reader.Objects(context.Background(), "fake"))
		require.NoError(t, err)
		require.Len(t, objects, 1)

		actual, err := result.Collect(reader.Streams(context.Background(), objects[0]))
		require.NoError(t, err)

		// TODO(rfratto): reenable once sorting is reintroduced.
		_ = actual
		// require.Equal(t, sortStreams(t, streams), actual)
	})
}

// Test_Builder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func Test_Builder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	bucket := objstore.NewInMemBucket()

	// Create a tiny builder which flushes a lot of objects and pages to properly
	// test the builder.
	builderConfig := BuilderConfig{
		SHAPrefixSize: 2,

		TargetPageSize:   2048,
		TargetObjectSize: 4096,
	}

	builder, err := NewBuilder(builderConfig, bucket, "fake")
	require.NoError(t, err)

	for {
		require.NoError(t, ctx.Err())

		err := builder.Append(logproto.Stream{
			Labels: `{cluster="test",app="foo"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now().UTC(),
				Line:      strings.Repeat("a", 1024),
			}},
		})
		if errors.Is(err, ErrBufferFull) {
			break
		}
		require.NoError(t, err)
	}
}

// sortStreams returns a new slice of streams where entries in individual
// streams are sorted by timestamp and structured metadata are sorted by key.
// The order of streams is preserved.
func sortStreams(t *testing.T, streams []logproto.Stream) []logproto.Stream {
	t.Helper()

	res := make([]logproto.Stream, len(streams))
	for i, in := range streams {
		labels, err := syntax.ParseLabels(in.Labels)
		require.NoError(t, err)

		res[i] = logproto.Stream{
			Labels:  labels.String(),
			Entries: slices.Clone(in.Entries),
			Hash:    labels.Hash(),
		}

		for j, ent := range res[i].Entries {
			res[i].Entries[j].StructuredMetadata = slices.Clone(ent.StructuredMetadata)
			slices.SortFunc(res[i].Entries[j].StructuredMetadata, func(i, j push.LabelAdapter) int {
				return cmp.Compare(i.Name, j.Name)
			})
		}

		slices.SortFunc(res[i].Entries, func(i, j push.Entry) int {
			switch {
			case i.Timestamp.Before(j.Timestamp):
				return -1

			case i.Timestamp.After(j.Timestamp):
				return 1

			default:
				return 0
			}
		})
	}

	return res
}
