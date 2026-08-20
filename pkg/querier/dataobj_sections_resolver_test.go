package querier

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	dskitmetrics "github.com/grafana/dskit/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type fakeQuerierMetastore struct {
	metastore.Metastore
	sections func(metastore.SectionsRequest) (metastore.SectionsResponse, error)
}

func (f *fakeQuerierMetastore) Sections(_ context.Context, req metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	return f.sections(req)
}

type fakeGatewayClient struct {
	mu         sync.Mutex
	reqs       []*logproto.ResolveDataObjectSectionsRequest
	respByFrom map[int64]*logproto.ResolveDataObjectSectionsResponse
	errByFrom  map[int64]error
	err        error
}

func (f *fakeGatewayClient) ResolveDataObjectSections(_ context.Context, in *logproto.ResolveDataObjectSectionsRequest) (*logproto.ResolveDataObjectSectionsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, in)
	if f.err != nil {
		return nil, f.err
	}
	if e, ok := f.errByFrom[int64(in.From)]; ok {
		return nil, e
	}
	if r, ok := f.respByFrom[int64(in.From)]; ok {
		return r, nil
	}
	return &logproto.ResolveDataObjectSectionsResponse{}, nil
}

func resolvedObject(path string, section int64, ids ...int64) logproto.ResolvedDataObject {
	return logproto.ResolvedDataObject{
		ObjectPath: path,
		Sections:   []logproto.ResolvedDataObjectSection{{SectionIdx: section, StreamIds: ids}},
	}
}

func mustMatchers() []*labels.Matcher {
	return []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
}

func windowMillis(t time.Time) int64 { return int64(model.TimeFromUnixNano(t.UnixNano())) }

func TestMetastoreSectionsResolver_Passthrough(t *testing.T) {
	want := []*metastore.DataobjSectionDescriptor{{SectionKey: metastore.SectionKey{ObjectPath: "o", SectionIdx: 0}, StreamIDs: []int64{1}}}
	r := metastoreSectionsResolver{ms: &fakeQuerierMetastore{sections: func(metastore.SectionsRequest) (metastore.SectionsResponse, error) {
		return metastore.SectionsResponse{Sections: want}, nil
	}}}
	got, err := r.resolveSections(context.Background(), time.Unix(0, 0), time.Unix(3600, 0), mustMatchers(), nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestIndexGatewaySectionsResolver_resolveSections(t *testing.T) {
	start := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	w1 := windowMillis(start)
	w2 := windowMillis(start.Add(12 * time.Hour))

	t.Run("splits by window", func(t *testing.T) {
		end := start.Add(18 * time.Hour) // spans windows [00:00,12:00) and [12:00,24:00)

		client := &fakeGatewayClient{respByFrom: map[int64]*logproto.ResolveDataObjectSectionsResponse{
			w1: {Objects: []logproto.ResolvedDataObject{resolvedObject("obj-1", 0, 1, 2)}},
			w2: {Objects: []logproto.ResolvedDataObject{resolvedObject("obj-2", 3, 9)}},
		}}
		reg := prometheus.NewRegistry()
		r := newDataObjSectionsResolver(&fakeQuerierMetastore{}, client, reg, log.NewNopLogger())

		got, err := r.resolveSections(context.Background(), start, end, mustMatchers(), nil)
		require.NoError(t, err)

		// The per-window RPCs fan out concurrently, so requests arrive in any order.
		require.Len(t, client.reqs, 2)
		froms := make([]int64, len(client.reqs))
		for i, req := range client.reqs {
			froms[i] = int64(req.From)
			require.Equal(t, int64(req.From)+int64(12*time.Hour/time.Millisecond), int64(req.Through)) // through == from + 12h
		}
		require.ElementsMatch(t, []int64{w1, w2}, froms)

		require.Len(t, got, 2)
		paths := []string{got[0].ObjectPath, got[1].ObjectPath}
		require.ElementsMatch(t, []string{"obj-1", "obj-2"}, paths)

		mfm, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		require.Equal(t, float64(0), mfm.SumCounters("loki_querier_dataobj_section_resolution_fallbacks_total")) // the gateway served it, no fallback
	})

	t.Run("dedups across windows", func(t *testing.T) {
		end := start.Add(18 * time.Hour)

		// The same (object, section) is returned by both windows with overlapping stream IDs.
		client := &fakeGatewayClient{respByFrom: map[int64]*logproto.ResolveDataObjectSectionsResponse{
			w1: {Objects: []logproto.ResolvedDataObject{resolvedObject("obj", 0, 1, 2)}},
			w2: {Objects: []logproto.ResolvedDataObject{resolvedObject("obj", 0, 2, 3)}},
		}}
		reg := prometheus.NewRegistry()
		r := newDataObjSectionsResolver(&fakeQuerierMetastore{}, client, reg, log.NewNopLogger())

		got, err := r.resolveSections(context.Background(), start, end, mustMatchers(), nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "obj", got[0].ObjectPath)
		require.ElementsMatch(t, []int64{1, 2, 3}, got[0].StreamIDs)

		mfm, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		require.Equal(t, float64(0), mfm.SumCounters("loki_querier_dataobj_section_resolution_fallbacks_total")) // the gateway served it, no fallback
	})

	t.Run("falls back to metastore on error", func(t *testing.T) {
		end := start.Add(6 * time.Hour)

		fallbackSections := []*metastore.DataobjSectionDescriptor{{SectionKey: metastore.SectionKey{ObjectPath: "fallback", SectionIdx: 7}, StreamIDs: []int64{5}}}
		ms := &fakeQuerierMetastore{sections: func(metastore.SectionsRequest) (metastore.SectionsResponse, error) {
			return metastore.SectionsResponse{Sections: fallbackSections}, nil
		}}
		client := &fakeGatewayClient{err: errors.New("gateway down")}
		reg := prometheus.NewRegistry()
		r := newDataObjSectionsResolver(ms, client, reg, log.NewNopLogger())

		got, err := r.resolveSections(context.Background(), start, end, mustMatchers(), nil)
		require.NoError(t, err)
		require.Equal(t, fallbackSections, got)

		mfm, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		require.Equal(t, float64(1), mfm.SumCounters("loki_querier_dataobj_section_resolution_fallbacks_total")) // the gateway error was counted as a fallback
	})

	t.Run("partial window failure falls back over the whole range", func(t *testing.T) {
		end := start.Add(18 * time.Hour) // two windows

		// First window succeeds; second window errors. The resolver must discard the partial gateway
		// result and return the metastore's full-range result (no under-resolve, no double-count).
		client := &fakeGatewayClient{
			respByFrom: map[int64]*logproto.ResolveDataObjectSectionsResponse{
				w1: {Objects: []logproto.ResolvedDataObject{resolvedObject("gateway-only", 0, 1)}},
			},
			errByFrom: map[int64]error{w2: errors.New("second window down")},
		}
		fallbackSections := []*metastore.DataobjSectionDescriptor{{SectionKey: metastore.SectionKey{ObjectPath: "metastore", SectionIdx: 0}, StreamIDs: []int64{9}}}
		ms := &fakeQuerierMetastore{sections: func(metastore.SectionsRequest) (metastore.SectionsResponse, error) {
			return metastore.SectionsResponse{Sections: fallbackSections}, nil
		}}
		reg := prometheus.NewRegistry()
		r := newDataObjSectionsResolver(ms, client, reg, log.NewNopLogger())

		got, err := r.resolveSections(context.Background(), start, end, mustMatchers(), nil)
		require.NoError(t, err)
		require.Equal(t, fallbackSections, got) // exactly the metastore result, not merged with the first window

		mfm, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		require.Equal(t, float64(1), mfm.SumCounters("loki_querier_dataobj_section_resolution_fallbacks_total")) // the partial failure was counted as a fallback
	})

	t.Run("gateway and fallback both fail records outcome=error", func(t *testing.T) {
		end := start.Add(6 * time.Hour)

		ms := &fakeQuerierMetastore{sections: func(metastore.SectionsRequest) (metastore.SectionsResponse, error) {
			return metastore.SectionsResponse{}, errors.New("metastore down")
		}}
		client := &fakeGatewayClient{err: errors.New("gateway down")}
		reg := prometheus.NewRegistry()
		r := newDataObjSectionsResolver(ms, client, reg, log.NewNopLogger())

		_, err := r.resolveSections(context.Background(), start, end, mustMatchers(), nil)
		require.Error(t, err) // gateway failed, then the metastore fallback also failed

		mfm, gerr := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, gerr)
		h, gerr := dskitmetrics.FindHistogramWithNameAndLabels(mfm, "loki_querier_dataobj_sections_resolve_duration_seconds", "outcome", "error")
		require.NoError(t, gerr)
		require.Equal(t, uint64(1), h.GetSampleCount())
	})
}

func TestNewDataObjSectionsResolver_Selection(t *testing.T) {
	require.IsType(t, metastoreSectionsResolver{}, newDataObjSectionsResolver(&fakeQuerierMetastore{}, nil, nil, log.NewNopLogger()))
	require.IsType(t, indexGatewaySectionsResolver{}, newDataObjSectionsResolver(&fakeQuerierMetastore{}, &fakeGatewayClient{}, nil, log.NewNopLogger()))
}

func TestIndexGatewaySectionsResolver_Metrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	ms := &fakeQuerierMetastore{sections: func(metastore.SectionsRequest) (metastore.SectionsResponse, error) {
		return metastore.SectionsResponse{}, nil // fallback succeeds with an empty result
	}}
	client := &fakeGatewayClient{err: errors.New("gateway down")}
	r := newDataObjSectionsResolver(ms, client, reg, log.NewNopLogger())

	start := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	_, err := r.resolveSections(context.Background(), start, start.Add(6*time.Hour), mustMatchers(), nil)
	require.NoError(t, err) // the gateway failed but the metastore fallback succeeded

	// The fallback is counted, and the successful (post-fallback) resolution is timed as outcome=success.
	mfm, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
	require.NoError(t, err)
	require.Equal(t, float64(1), mfm.SumCounters("loki_querier_dataobj_section_resolution_fallbacks_total"))
	h, err := dskitmetrics.FindHistogramWithNameAndLabels(mfm, "loki_querier_dataobj_sections_resolve_duration_seconds", "outcome", "success")
	require.NoError(t, err)
	require.Equal(t, uint64(1), h.GetSampleCount())
}

func TestIndexGatewaySectionsResolver_Cancellation(t *testing.T) {
	reg := prometheus.NewRegistry()
	fallbackCalled := false
	ms := &fakeQuerierMetastore{sections: func(metastore.SectionsRequest) (metastore.SectionsResponse, error) {
		fallbackCalled = true
		return metastore.SectionsResponse{}, nil
	}}
	client := &fakeGatewayClient{err: status.Error(codes.Canceled, "context canceled")}
	r := newDataObjSectionsResolver(ms, client, reg, log.NewNopLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	_, err := r.resolveSections(ctx, start, start.Add(6*time.Hour), mustMatchers(), nil)

	require.Error(t, err)
	require.False(t, fallbackCalled, "a cancelled query must not trigger the metastore fallback")
	mfm, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
	require.NoError(t, err)
	require.Equal(t, float64(0), mfm.SumCounters("loki_querier_dataobj_section_resolution_fallbacks_total"))
	h, err := dskitmetrics.FindHistogramWithNameAndLabels(mfm, "loki_querier_dataobj_sections_resolve_duration_seconds", "outcome", "canceled")
	require.NoError(t, err)
	require.Equal(t, uint64(1), h.GetSampleCount())
}
