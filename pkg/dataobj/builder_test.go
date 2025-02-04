package dataobj

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = BuilderConfig{
	SHAPrefixSize: 2,

	TargetPageSize:    2048,
	TargetObjectSize:  4096,
	TargetSectionSize: 4096,

	BufferSize: 2048 * 8,
}

func TestBuilder(t *testing.T) {
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
		builder, err := NewBuilder(testBuilderConfig, bucket, "fake")
		require.NoError(t, err)

		for _, entry := range streams {
			require.NoError(t, builder.Append(entry))
		}
		_, err = builder.Flush(context.Background())
		require.NoError(t, err)
	})

	t.Run("Read", func(t *testing.T) {
		objects, err := result.Collect(listObjects(context.Background(), bucket, "fake"))
		require.NoError(t, err)
		require.Len(t, objects, 1)

		obj := FromBucket(bucket, objects[0])
		md, err := obj.Metadata(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, md.StreamsSections)
		require.Equal(t, 1, md.LogsSections)
	})
}

// TestBuilder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func TestBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	bucket := objstore.NewInMemBucket()

	builder, err := NewBuilder(testBuilderConfig, bucket, "fake")
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

func listObjects(ctx context.Context, bucket objstore.Bucket, tenant string) result.Seq[string] {
	tenantPath := fmt.Sprintf("tenant-%s/objects/", tenant)

	return result.Iter(func(yield func(string) bool) error {
		errIterationStopped := errors.New("iteration stopped")

		err := bucket.Iter(ctx, tenantPath, func(name string) error {
			if !yield(name) {
				return errIterationStopped
			}
			return nil
		}, objstore.WithRecursiveIter())

		switch {
		case errors.Is(err, errIterationStopped):
			return nil
		default:
			return err
		}
	})
}
