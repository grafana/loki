package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// TsdbCreator accepts writes and builds TSDBs.
type TsdbCreator struct {
	// Function to build a TSDB from the current state

	mtx    sync.RWMutex
	shards int
	heads  *tenantHeads
}

// new creates a new HeadManager
func newTsdbCreator() *TsdbCreator {
	m := &TsdbCreator{
		shards: 1 << 5, // 32 shards
	}
	m.reset()

	return m
}

// reset updates heads
func (m *TsdbCreator) reset() {
	m.heads = newTenantHeads(m.shards)
}

// Append adds a new series for the given user
func (m *TsdbCreator) Append(userID string, ls labels.Labels, fprint uint64, chks index.ChunkMetas) error {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	// TODO(owen-d): safe to remove?
	// Remove __name__="logs" as it's not needed in TSDB
	b := labels.NewBuilder(ls)
	b.Del(labels.MetricName)
	ls = b.Labels()

	// Just append to heads, no WAL needed
	m.heads.Append(userID, ls, fprint, chks)
	return nil
}

type chunkInfo struct {
	chunkMetas index.ChunkMetas
	tsdbFormat int
}

type tsdbWithID struct {
	bucket model.Time
	data   []byte
	id     tsdb.Identifier
}

// Create builds a TSDB from the current state using the provided mkTsdb function
func (m *TsdbCreator) create(ctx context.Context, nodeName string, tableRanges []config.TableRange) ([]tsdbWithID, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	type key struct {
		bucket model.Time
		prefix string
	}
	periods := make(map[key]*tsdb.Builder)

	if err := m.heads.forAll(
		func(user string, ls labels.Labels, fp uint64, chks index.ChunkMetas) error {
			// chunks may overlap index period bounds, in which case they're written to multiple
			pds := make(map[key]chunkInfo)
			for _, chk := range chks {
				idxBuckets := tsdb.IndexBuckets(chk.From(), chk.Through(), tableRanges)

				for _, bucket := range idxBuckets {
					k := key{
						bucket: bucket.BucketStart,
						prefix: bucket.Prefix,
					}
					chkinfo := pds[k]
					chkinfo.chunkMetas = append(chkinfo.chunkMetas, chk)
					chkinfo.tsdbFormat = bucket.TsdbFormat
					pds[k] = chkinfo
				}
			}

			// Embed the tenant label into TSDB
			lb := labels.NewBuilder(ls)
			lb.Set(index.TenantLabel, user)
			withTenant := lb.Labels()

			// Add the chunks to all relevant builders
			for pd, chkinfo := range pds {
				matchingChks := chkinfo.chunkMetas
				b, ok := periods[pd]
				if !ok {
					b = tsdb.NewBuilder(chkinfo.tsdbFormat)
					periods[pd] = b
				}

				b.AddSeries(
					withTenant,
					// use the fingerprint without the added tenant label
					// so queries route to the chunks which actually exist.
					model.Fingerprint(fp),
					matchingChks,
				)
			}

			return nil
		},
	); err != nil {
		level.Error(util_log.Logger).Log("err", err.Error(), "msg", "building TSDB")
		return nil, err
	}

	now := time.Now()
	res := make([]tsdbWithID, 0, len(periods))

	for p, b := range periods {

		level.Debug(util_log.Logger).Log(
			"msg", "building tsdb for period",
			"pd", p,
		)

		// build+move tsdb to multitenant dir
		start := time.Now()
		dst, data, err := b.BuildInMemory(
			ctx,
			func(_, _ model.Time, _ uint32) tsdb.Identifier {
				return tsdb.NewPrefixedIdentifier(
					tsdb.MultitenantTSDBIdentifier{
						NodeName: nodeName,
						Ts:       now,
					},
					p.prefix,
					"",
				)
			},
		)

		if err != nil {
			return nil, err
		}

		level.Debug(util_log.Logger).Log(
			"msg", "finished building tsdb for period",
			"pd", p,
			"dst", dst.Path(),
			"duration", time.Since(start),
		)
		res = append(res, tsdbWithID{
			bucket: p.bucket,
			id:     dst,
			data:   data,
		})
	}

	m.reset()
	return res, nil
}

// tenantHeads manages per-tenant series
type tenantHeads struct {
	shards  int
	locks   []sync.RWMutex
	tenants []map[string]*Head
}

func newTenantHeads(shards int) *tenantHeads {
	t := &tenantHeads{
		shards:  shards,
		locks:   make([]sync.RWMutex, shards),
		tenants: make([]map[string]*Head, shards),
	}
	for i := range t.tenants {
		t.tenants[i] = make(map[string]*Head)
	}
	return t
}

func (t *tenantHeads) Append(userID string, ls labels.Labels, fprint uint64, chks index.ChunkMetas) {
	head := t.getOrCreateTenantHead(userID)
	head.Append(ls, fprint, chks)
}

func (t *tenantHeads) getOrCreateTenantHead(userID string) *Head {
	idx := t.shardForTenant(userID)
	mtx := &t.locks[idx]

	// Fast path: return existing head
	mtx.RLock()
	head, ok := t.tenants[idx][userID]
	mtx.RUnlock()
	if ok {
		return head
	}

	// Slow path: create new head
	mtx.Lock()
	defer mtx.Unlock()

	head, ok = t.tenants[idx][userID]
	if !ok {
		head = NewHead(userID)
		t.tenants[idx][userID] = head
	}
	return head
}

func (t *tenantHeads) shardForTenant(userID string) uint64 {
	return xxhash.Sum64String(userID) & uint64(t.shards-1)
}

// forAll iterates through all series in all tenant heads
func (t *tenantHeads) forAll(fn func(user string, ls labels.Labels, fp uint64, chks index.ChunkMetas) error) error {
	for i, shard := range t.tenants {
		t.locks[i].RLock()
		defer t.locks[i].RUnlock()

		for user, tenant := range shard {
			if err := tenant.forAll(func(ls labels.Labels, fp uint64, chks index.ChunkMetas) error {
				return fn(user, ls, fp, chks)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Head manages series for a single tenant
type Head struct {
	userID string
	series map[uint64]*series
	mtx    sync.RWMutex
}

type series struct {
	labels labels.Labels
	chks   []index.ChunkMeta
}

func NewHead(userID string) *Head {
	return &Head{
		userID: userID,
		series: make(map[uint64]*series),
	}
}

func (h *Head) Append(ls labels.Labels, fp uint64, chks index.ChunkMetas) {
	h.mtx.Lock()
	defer h.mtx.Unlock()

	s, ok := h.series[fp]
	if !ok {
		s = &series{labels: ls}
		h.series[fp] = s
	}
	s.chks = append(s.chks, chks...)
}

func (h *Head) forAll(fn func(ls labels.Labels, fp uint64, chks index.ChunkMetas) error) error {
	h.mtx.RLock()
	defer h.mtx.RUnlock()

	for fp, s := range h.series {
		if err := fn(s.labels, fp, s.chks); err != nil {
			return err
		}
	}
	return nil
}

type uploader struct {
	store *MultiStore
}

func newUploader(store *MultiStore) *uploader {
	return &uploader{store: store}
}

func (u *uploader) Put(ctx context.Context, db tsdbWithID) error {
	client, err := u.store.GetStoreFor(db.bucket)
	if err != nil {
		return err
	}

	reader := bytes.NewReader(db.data)
	gzipPool := compression.GetWriterPool(compression.GZIP)
	buf := bytes.NewBuffer(make([]byte, 0, 1<<20))
	compressedWriter := gzipPool.GetWriter(buf)
	defer gzipPool.PutWriter(compressedWriter)

	_, err = io.Copy(compressedWriter, reader)
	if err != nil {
		return err
	}

	err = compressedWriter.Close()
	if err != nil {
		return err
	}

	return client.PutObject(ctx, buildFileName(db.id.Path()), buf)
}

func buildFileName(indexName string) string {
	return fmt.Sprintf("%s.gz", indexName)
}
