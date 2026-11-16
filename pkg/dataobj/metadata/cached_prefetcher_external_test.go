// These tests are in package metadata_test because they use objtest, which imports pkg/dataobj, which
// imports metadata. A test in package metadata that imports objtest is an import cycle.

package metadata_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/metrics"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// headBytes is the smallest first read of a cold open.
const headBytes = 16 * 1024

type testObject struct {
	bucket objstore.Bucket
	path   string
	raw    []byte

	// metadataRegionEnd is where the metadata regions of all sections end.
	metadataRegionEnd int64
}

// newTestObject builds an object with many small logs sections, so its metadata is larger than the
// first read of a cold open.
func newTestObject(t *testing.T) *testObject {
	t.Helper()
	return newTestObjectWithStreams(t, 40)
}

// newTestObjectWithStreams builds a test object with the given number of streams. Objects with a
// different number of streams have different paths and different metadata.
func newTestObjectWithStreams(t *testing.T, streams int) *testObject {
	t.Helper()

	builder := objtest.NewBuilder(t, objtest.WithTargetSectionSize(flagext.Bytes(8*1024)))
	ctx := user.InjectOrgID(t.Context(), objtest.Tenant)
	for i := range streams {
		entries := make([]push.Entry, 0, 200)
		for j := range 200 {
			entries = append(entries, push.Entry{
				Timestamp: time.Unix(int64(j+1), 0).UTC(),
				Line:      fmt.Sprintf("line %d %d with some more text to fill sections", i, j),
				StructuredMetadata: []push.LabelAdapter{
					{Name: "trace_id", Value: fmt.Sprintf("t%d-%d", i, j)},
					{Name: "level", Value: "info"},
					{Name: fmt.Sprintf("only_in_%d", i%3), Value: "x"},
				},
			})
		}
		builder.Append(ctx, logproto.Stream{Labels: fmt.Sprintf(`{app="a", idx="%d"}`, i), Entries: entries})
	}
	builder.Close()

	resp, err := builder.Metastore().Sections(ctx, metastore.SectionsRequest{
		Start:    time.Unix(0, 0).UTC(),
		End:      time.Unix(1000, 0).UTC(),
		Matchers: syntax.MustParseLogSelector(`{app="a"}`, true).Matchers(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Sections)

	obj := &testObject{bucket: builder.Location().Bucket, path: resp.Sections[0].ObjectPath}
	rc, err := obj.bucket.Get(ctx, obj.path)
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()
	obj.raw, err = io.ReadAll(rc)
	require.NoError(t, err)

	obj.metadataRegionEnd = objtest.MetadataRegionEnd(t, obj.bucket, obj.path)
	require.Greater(t, obj.metadataRegionEnd, int64(headBytes), "the metadata must not fit in the first read")
	return obj
}

func (o *testObject) request(prefetchSet metadata.PrefetchSet) metadata.PrefetchRequest {
	return metadata.PrefetchRequest{Path: o.path, PrefetchSet: prefetchSet, Source: &testSource{bucket: o.bucket, path: o.path}}
}

// counterWithLabels returns the value of the one counter of the family name that has the labels.
// The dskit metrics package has no such helper, so it builds on FindMetricsInFamilyMatchingLabels.
func counterWithLabels(t *testing.T, mfm metrics.MetricFamilyMap, name string, labelNamesAndValues ...string) float64 {
	t.Helper()

	matching := metrics.FindMetricsInFamilyMatchingLabels(mfm[name], labelNamesAndValues...)
	require.Len(t, matching, 1, "series of %s with labels %v", name, labelNamesAndValues)
	return matching[0].GetCounter().GetValue()
}

type fakeCache struct {
	mu     sync.Mutex
	values map[string][]byte

	// failReads and failWrites make the reads and the writes return an error.
	failReads, failWrites bool

	setItems int
}

func newFakeCache() *fakeCache { return &fakeCache{values: map[string][]byte{}} }

func (s *fakeCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failReads {
		return nil, nil, nil, errors.New("cache is down")
	}
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			found, bufs = append(found, key), append(bufs, value)
		} else {
			missing = append(missing, key)
		}
	}
	return found, bufs, missing, nil
}

func (s *fakeCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setItems += len(keys)
	if s.failWrites {
		return errors.New("cache is down")
	}
	for i, key := range keys {
		s.values[key] = bufs[i]
	}
	return nil
}

func (s *fakeCache) Stop() {}

func (s *fakeCache) GetCacheType() stats.CacheType { return "fake" }

func (s *fakeCache) sets() int { s.mu.Lock(); defer s.mu.Unlock(); return s.setItems }

// entry returns the name of an entry whose cache item holds an unhashed key with part after the "#",
// or "" when the cache has none.
func (s *fakeCache) entry(part string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, value := range s.values {
		if strings.Contains(itemKey(value), "#"+part) {
			return name
		}
	}
	return ""
}

// corrupt changes the last byte of an entry whose cache item holds an unhashed key with part after
// the "#".
func (s *fakeCache) corrupt(part string) {
	name := s.entry(part)
	s.mu.Lock()
	defer s.mu.Unlock()
	value := bytes.Clone(s.values[name])
	value[len(value)-1] ^= 0xff
	s.values[name] = value
}

// itemKey returns the unhashed cache key that a cache item holds, or "" when the item is malformed. A
// cache item starts with a version byte, a codec byte, the raw size as a uvarint and a 4 byte checksum.
// The key follows with its length as a uvarint.
func itemKey(item []byte) string {
	if len(item) < 2 {
		return ""
	}
	_, n := binary.Uvarint(item[2:])
	if n <= 0 || len(item) < 2+n+4 {
		return ""
	}
	rest := item[2+n+4:]
	length, n := binary.Uvarint(rest)
	if n <= 0 || length > uint64(len(rest)-n) {
		return ""
	}
	return string(rest[n : n+int(length)])
}

type readCall struct{ offset, length int64 }

// testSource reads an object from a bucket and records the reads.
type testSource struct {
	bucket objstore.Bucket
	path   string

	// data, when set, replaces the object.
	data []byte

	// block makes a read wait until its context ends. gate makes a read wait until it is closed.
	block bool
	gate  chan struct{}

	// err makes every read fail. closeErr makes closing the reader of a read fail.
	err      error
	closeErr error

	mu    sync.Mutex
	reads []readCall
}

func (s *testSource) ReadRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	s.mu.Lock()
	s.reads = append(s.reads, readCall{offset, length})
	s.mu.Unlock()

	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.data != nil {
		end := min(offset+length, int64(len(s.data)))
		return s.withCloseErr(io.NopCloser(bytes.NewReader(s.data[min(offset, end):end]))), nil
	}
	rc, err := s.bucket.GetRange(ctx, s.path, offset, length)
	if err != nil {
		return nil, err
	}
	return s.withCloseErr(rc), nil
}

// withCloseErr returns rc, or a reader whose Close also returns s.closeErr.
func (s *testSource) withCloseErr(rc io.ReadCloser) io.ReadCloser {
	if s.closeErr == nil {
		return rc
	}
	return failingCloser{ReadCloser: rc, err: s.closeErr}
}

type failingCloser struct {
	io.ReadCloser
	err error
}

func (c failingCloser) Close() error {
	return errors.Join(c.ReadCloser.Close(), c.err)
}

func (s *testSource) calls() []readCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]readCall(nil), s.reads...)
}
