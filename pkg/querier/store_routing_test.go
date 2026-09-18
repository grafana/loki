package querier

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

func TestDataobjBandEnd(t *testing.T) {
	const storeEnd = model.Time(1_000_000)

	for _, tc := range []struct {
		name            string
		dataobjEnd      model.Time
		ingesterQueried bool
		want            model.Time
	}{
		// Data objects stop before the store end: the recent slice genuinely lives on chunks, so the
		// band ends at dataobjEnd regardless of whether an ingester query abuts the end.
		{"dataobj stops before end, ingester queried", storeEnd - 5000, true, storeEnd - 5000},
		{"dataobj stops before end, no ingester", storeEnd - 5000, false, storeEnd - 5000},

		// Data objects reach the store end and an ingester query abuts it (recent query): reserve the
		// boundary sample for the chunk store so it deduplicates against the ingester.
		{"dataobj at end, ingester queried", storeEnd, true, storeEnd - 1},
		{"dataobj past end, ingester queried", storeEnd + 5000, true, storeEnd - 1},

		// Data objects reach the store end and no ingester query abuts it (historical query): data
		// objects serve up to the end, so the boundary sample never goes to the chunk store.
		{"dataobj at end, no ingester", storeEnd, false, storeEnd},
		{"dataobj past end, no ingester", storeEnd + 5000, false, storeEnd},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, dataobjBandEnd(tc.dataobjEnd, storeEnd, tc.ingesterQueried))
		})
	}
}

// TestStoreForSampleParams_BoundaryRouting proves the fix behaviorally: for a store query whose end
// sits inside the data-object-available window, the chunk store is untouched when no ingester query
// abuts the end (historical), and only serves the boundary sample when one does (recent).
func TestStoreForSampleParams_BoundaryRouting(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	// End 25h ago: older than the 24h storage lag, so data objects cover it (dataobjEnd = now-24h is
	// later). The store-side query is thus fully inside the data-object window.
	start, end := now.Add(-26*time.Hour), now.Add(-25*time.Hour)
	params := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
		Start:    start,
		End:      end,
		Selector: `{app="x"}`,
		Order:    logproto.SAMPLE_ORDER_BY_STREAM,
	}}

	newQ := func(chunk, dataobj Store) *SingleTenantQuerier {
		return &SingleTenantQuerier{store: chunk, dataObjStore: dataobj, cfg: Config{
			IngesterQueryStoreMaxLookback: 24 * time.Hour,
			DataObjectsStorageLag:         24 * time.Hour,
			DataObjectsStorageStartDate:   flagext.Time(now.Add(-30 * 24 * time.Hour)),
		}}
	}

	t.Run("historical: chunk store untouched", func(t *testing.T) {
		chunk, dataobj := &recordingSampleStore{}, &recordingSampleStore{}
		_, err := newQ(chunk, dataobj).storeForSampleParams(params, false).SelectSamples(ctx, params)
		require.NoError(t, err)
		require.NotEmpty(t, dataobj.calls, "data objects should serve the range")
		require.Empty(t, chunk.calls, "a historical query must not touch the chunk store")
	})

	t.Run("recent: boundary reserved for the chunk store", func(t *testing.T) {
		chunk, dataobj := &recordingSampleStore{}, &recordingSampleStore{}
		_, err := newQ(chunk, dataobj).storeForSampleParams(params, true).SelectSamples(ctx, params)
		require.NoError(t, err)
		require.NotEmpty(t, dataobj.calls, "data objects should serve the bulk of the range")
		require.NotEmpty(t, chunk.calls, "a recent query must route the boundary sample to the chunk store")
	})
}
