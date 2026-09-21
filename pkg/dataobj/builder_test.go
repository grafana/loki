package dataobj_test

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestBuilder_preserve_section_version(t *testing.T) {
	builder := dataobj.NewBuilder(nil)
	err := builder.Append(fakeSectionBuilder{
		SectionType: dataobj.SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "custom-section",
			Version:   42,
		},
	})
	require.NoError(t, err)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.Len(t, obj.Sections(), 1, "expected only one section in the object")
	require.Equal(t, uint32(42), obj.Sections()[0].Type.Version, "expected section version to be preserved")
}

func TestBuilder_preserve_extension(t *testing.T) {
	builder := dataobj.NewBuilder(nil)
	err := builder.Append(fakeSectionBuilder{
		SectionType: dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"},
		FlushFunc: func(w dataobj.SectionWriter) (n int64, err error) {
			opts := &dataobj.WriteSectionOptions{
				ExtensionData: []byte("test extension"),
			}
			return w.WriteSection(opts, []byte("test data"), []byte("test metadata"))
		},
	})
	require.NoError(t, err)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.Len(t, obj.Sections(), 1, "expected only one section in the object")
	require.Equal(t, []byte("test extension"), obj.Sections()[0].Reader.ExtensionData())
}

func TestBuilder_preserve_tenant(t *testing.T) {
	builder := dataobj.NewBuilder(nil)
	err := builder.Append(fakeSectionBuilder{
		SectionType: dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"},
		FlushFunc: func(w dataobj.SectionWriter) (n int64, err error) {
			opts := &dataobj.WriteSectionOptions{
				Tenant: "my-test-tenant",
			}
			return w.WriteSection(opts, []byte("test data"), []byte("test metadata"))
		},
	})
	require.NoError(t, err)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.Len(t, obj.Sections(), 1, "expected only one section in the object")
	require.Equal(t, "my-test-tenant", obj.Sections()[0].Tenant)
}

// Buffered sections are released by whoever owns them when the builder is done
// with them: the returned closer after a successful flush, the builder itself
// on every other path.
func TestBuilder_ReleasesScratchStorage(t *testing.T) {
	appendSection := func(t *testing.T, b *dataobj.Builder) {
		t.Helper()
		require.NoError(t, b.Append(fakeSectionBuilder{
			SectionType: dataobj.SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"},
			FlushFunc: func(w dataobj.SectionWriter) (n int64, err error) {
				return w.WriteSection(nil, []byte("test data"), []byte("test metadata"))
			},
		}))
	}

	t.Run("reset", func(t *testing.T) {
		store := newTrackingStore(false)
		builder := dataobj.NewBuilder(store)
		appendSection(t, builder)

		builder.Reset()
		store.requireReleased(t)
	})

	t.Run("successful flush", func(t *testing.T) {
		store := newTrackingStore(false)
		builder := dataobj.NewBuilder(store)
		appendSection(t, builder)

		_, closer, err := builder.Flush()
		require.NoError(t, err)
		store.requireHeld(t, "the object must stay readable until the caller closes it")

		require.NoError(t, closer.Close())
		store.requireReleased(t)
	})

	t.Run("failed flush", func(t *testing.T) {
		store := newTrackingStore(true)
		builder := dataobj.NewBuilder(store)
		appendSection(t, builder)

		_, closer, err := builder.Flush()
		require.ErrorContains(t, err, "error building object")
		require.Nil(t, closer)
		store.requireReleased(t)
	})
}

// trackingStore records every handle it hands out so tests can tell which ones
// were released. Reads optionally fail, which makes building an object from a
// flushed snapshot fail.
type trackingStore struct {
	scratch.Store

	handles  []scratch.Handle
	failRead bool
}

func newTrackingStore(failRead bool) *trackingStore {
	return &trackingStore{Store: scratch.NewMemory(), failRead: failRead}
}

func (s *trackingStore) Put(p []byte) scratch.Handle {
	h := s.Store.Put(p)
	s.handles = append(s.handles, h)
	return h
}

func (s *trackingStore) Read(h scratch.Handle) (io.ReadSeekCloser, error) {
	if s.failRead {
		return nil, errors.New("mock read error")
	}
	return s.Store.Read(h)
}

func (s *trackingStore) requireReleased(t *testing.T) {
	t.Helper()
	require.NotEmpty(t, s.handles, "no sections were buffered")
	for _, h := range s.handles {
		require.ErrorAs(t, s.Store.Remove(h), new(scratch.HandleNotFoundError), "handle %d was not released", h)
	}
}

func (s *trackingStore) requireHeld(t *testing.T, msg string) {
	t.Helper()
	require.NotEmpty(t, s.handles, "no sections were buffered")
	for _, h := range s.handles {
		r, err := s.Store.Read(h)
		require.NoError(t, err, msg)
		require.NoError(t, r.Close())
	}
}

type fakeSectionBuilder struct {
	SectionType dataobj.SectionType
	FlushFunc   func(w dataobj.SectionWriter) (n int64, err error)
	ResetFunc   func()
}

var _ dataobj.SectionBuilder = (*fakeSectionBuilder)(nil)

func (fake fakeSectionBuilder) Type() dataobj.SectionType { return fake.SectionType }

func (fake fakeSectionBuilder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	if fake.FlushFunc != nil {
		return fake.FlushFunc(w)
	}
	return w.WriteSection(nil, nil, nil)
}

func (fake fakeSectionBuilder) Reset() {
	if fake.ResetFunc != nil {
		fake.ResetFunc()
	}
}
