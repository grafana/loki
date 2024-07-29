package metastore

import (
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func Benchmark_metastoreState_applyAddBlock(t *testing.B) {
	anHourAgo := ulid.Timestamp(time.Now())
	workers := 1000
	workChan := make(chan struct{}, workers)
	wg := sync.WaitGroup{}
	m := &metastoreState{
		logger:        util_log.Logger,
		segmentsMutex: sync.Mutex{},
		segments:      make(map[string]*metastorepb.BlockMeta),
		db: &boltdb{
			logger: util_log.Logger,
			config: Config{
				DataDir: t.TempDir(),
				Raft: RaftConfig{
					Dir: t.TempDir(),
				},
			},
		},
	}

	err := m.db.open(false)
	require.NoError(t, err)

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			for range workChan {
				_, err = m.applyAddBlock(&metastorepb.AddBlockRequest{
					Block: &metastorepb.BlockMeta{
						Id: ulid.MustNew(anHourAgo, rand.Reader).String(),
					},
				})
				require.NoError(t, err)
			}
			wg.Done()
		}()
	}

	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		workChan <- struct{}{}
	}
	close(workChan)
	wg.Wait()
}
