package ingester

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
	loki_runtime "github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

type recreationRecordingWAL struct {
	noopWAL
	records [][]byte
}

func (w *recreationRecordingWAL) Log(r *wal.Record) error {
	if len(r.Series) > 0 {
		w.records = append(w.records, r.EncodeSeries(nil))
	}
	if len(r.RefEntries) > 0 {
		w.records = append(w.records, r.EncodeEntries(wal.CurrentEntriesRec, nil))
	}
	return nil
}

func TestWALReplayAfterStreamRecreation(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		name := "without checkpoint"
		if checkpoint {
			name = "with checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
			require.NoError(t, err)
			newIngester := func() *Ingester {
				cfg := defaultIngesterTestConfig(t)
				cfg.RetainPeriod = 0
				i, err := New(cfg, client.Config{}, &mockStore{
					chunks: map[string][]chunk.Chunk{},
				}, limits, loki_runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki,
					log.NewNopLogger(), nil, mockReadRingWithOneActiveIngester(), nil)
				require.NoError(t, err)
				return i
			}
			i := newIngester()
			inst, err := i.GetOrCreateInstance("test")
			require.NoError(t, err)
			recorded := &recreationRecordingWAL{}
			inst.wal = recorded
			ctx := user.InjectOrgID(context.Background(), "test")
			const selector = `{foo="bar"}`
			push := func(target *instance, labels string, second int64, line string) {
				require.NoError(t, target.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{{
					Labels:  labels,
					Entries: []logproto.Entry{{Timestamp: time.Unix(second, 0), Line: line}},
				}}}))
			}
			flushAndRemove := func(labels string) {
				s, ok := inst.streams.Load(labels)
				require.True(t, ok)
				require.NoError(t, i.flushUserSeries(ctx, "test", s.fp, true))
				i.removeFlushedChunks(inst, s, true)
				_, ok = inst.streams.Load(labels)
				require.False(t, ok)
			}
			push(inst, selector, 1, "before removal")
			flushAndRemove(selector)

			// Another removed stream can move the shared high-water mark ahead.
			// Replay must preserve the persisted counter even when it has gaps.
			const other = `{foo="other"}`
			push(inst, other, 1, "other one")
			push(inst, other, 2, "other two")
			push(inst, other, 3, "other three")
			flushAndRemove(other)
			push(inst, selector, 2, "after recreation")

			var series *Series
			if checkpoint {
				iterator := newIngesterSeriesIter(i).Iter()
				require.True(t, iterator.Next())
				series = iterator.Stream()
				require.False(t, iterator.Next())
			}

			replay := func() *Ingester {
				recovered := newIngester()
				recoverer := newIngesterRecoverer(recovered)
				if series != nil {
					require.NoError(t, recoverer.Series(series))
				}
				require.NoError(t, RecoverWAL(ctx, &MemoryWALReader{xs: recorded.records}, recoverer))
				recoverer.Close()
				return recovered
			}
			queryLines := func(target *Ingester) []string {
				result := mockQuerierServer{ctx: ctx}
				require.NoError(t, target.Query(&logproto.QueryRequest{
					Selector: selector, Limit: 100, Start: time.Unix(0, 0), End: time.Unix(10, 0),
					Plan: testutil.MustPlan(selector),
				}, &result))
				var lines []string
				for _, response := range result.resps {
					for _, stream := range response.Streams {
						for _, entry := range stream.Entries {
							lines = append(lines, entry.Line)
						}
					}
				}
				return lines
			}
			recovered := replay()
			expected := []string{"before removal", "after recreation"}
			if checkpoint {
				// The checkpoint only contains the recreated stream. Its first
				// incarnation was already flushed and is not in memory.
				expected = []string{"after recreation"}
			}
			require.ElementsMatch(t, expected, queryLines(recovered))

			recoveredInstance, err := recovered.GetOrCreateInstance("test")
			require.NoError(t, err)
			recoveredInstance.wal = recorded
			push(recoveredInstance, selector, 3, "after restart")
			expected = append(expected, "after restart")
			require.ElementsMatch(t, expected, queryLines(replay()))
		})
	}
}
