package dataobjread

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk"

	"github.com/grafana/loki/pkg/push"
)

// sampleRow is one emitted sample with its identity, for order-independent comparison. The read
// path emits samples in no order, so a test compares sets, never sequences.
type sampleRow struct {
	Labels       string
	TimestampSec int64
	Value        float64
	StreamHash   uint64
}

// objectsFixture is a bucket of data objects together with the descriptors the metastore
// resolves for a query over all of them.
type objectsFixture struct {
	bucket      objstore.Bucket
	descriptors []*metastore.DataobjSectionDescriptor
}

// newObjectsFixture writes streams for objtest.Tenant, plus optionally a stream for another
// tenant into the same object, and resolves every section they produced.
//
// It builds a real index, so the descriptors carry the section indexes and stream IDs a query
// would resolve. Use [createTestStoredObject] for a test that only needs an object in a bucket.
func newObjectsFixture(t *testing.T, otherTenant string, tenantStreams ...logproto.Stream) objectsFixture {
	t.Helper()

	builder := objtest.NewBuilder(t)
	ctx := user.InjectOrgID(t.Context(), objtest.Tenant)

	if otherTenant != "" {
		builder.AppendFor(ctx, otherTenant, logproto.Stream{
			Labels:  `{app="other"}`,
			Entries: []push.Entry{entry(t, 1, "theirs")},
		})
	}
	builder.Append(ctx, tenantStreams...)
	builder.Close()

	resp, err := builder.Metastore().Sections(ctx, metastore.SectionsRequest{
		Start:    at(0),
		End:      at(100),
		Matchers: syntax.MustParseLogSelector(`{app=~".+"}`, true).Matchers(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Sections, "the metastore resolved no section for the fixture")

	return objectsFixture{bucket: builder.Location().Bucket, descriptors: resp.Sections}
}

// denyEverythingFilterer rejects every stream, so a test can assert that access control is
// applied at all.
type denyEverythingFilterer struct{}

func (f denyEverythingFilterer) ForRequest(context.Context) chunk.Filterer { return f }

func (f denyEverythingFilterer) ShouldFilter(labels.Labels) bool { return true }

func (f denyEverythingFilterer) RequiredLabelNames() []string { return nil }

// epoch anchors every test timestamp, so a query range reads as plain seconds and the metastore's
// time filter has something stable to match.
var epoch = time.Unix(0, 0).UTC()

// at returns the given number of seconds after [epoch].
func at(second int) time.Time { return epoch.Add(time.Duration(second) * time.Second) }

// entry returns one log line at [at](second), with the given structured metadata as alternating
// name and value arguments.
func entry(t *testing.T, second int, line string, metadata ...string) push.Entry {
	t.Helper()
	require.Zero(t, len(metadata)%2, "metadata must be name and value pairs")

	e := push.Entry{Timestamp: at(second), Line: line}
	for i := 0; i+1 < len(metadata); i += 2 {
		e.StructuredMetadata = append(e.StructuredMetadata, push.LabelAdapter{Name: metadata[i], Value: metadata[i+1]})
	}
	return e
}

// streamHashOf returns the stream hash of a LogQL label set, which is what an emitted sample
// carries to identify its stream.
func streamHashOf(streamLabels string) uint64 {
	parsed, err := syntax.ParseLabels(streamLabels)
	if err != nil {
		panic(err)
	}
	return labels.StableHash(parsed)
}
