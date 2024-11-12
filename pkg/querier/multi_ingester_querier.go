package querier

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	index_stats "github.com/grafana/loki/v3/pkg/storage/stores/index/stats"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
)

type MultiIngesterConfig struct {
	MultiIngesterConfig []ring.LifecyclerConfig `yaml:"lifecyclers,omitempty"`
}

type MultiIngesterQuerier struct {
	services.Service
	cfg              *MultiIngesterConfig
	manager          *services.Manager
	ring             []*ring.Ring
	ingesterQueriers []storage.IIngesterQuerier
}

func NewMultiIngesterQuerier(cfg MultiIngesterConfig, clientCfg client.Config, querierCfg Config, shardCountFun func(userID string) int, namespace string) (storage.IIngesterQuerier, error) {
	var (
		err  error
		srvc = make([]services.Service, 0, len(cfg.MultiIngesterConfig))
		miq  = &MultiIngesterQuerier{
			cfg:              &cfg,
			ring:             make([]*ring.Ring, 0, len(cfg.MultiIngesterConfig)),
			ingesterQueriers: make([]storage.IIngesterQuerier, 0, len(cfg.MultiIngesterConfig)),
		}
	)

	for i, c := range cfg.MultiIngesterConfig {
		r, err := ring.New(c.RingConfig, "ingester", ingester.RingKey, util_log.Logger, prometheus.WrapRegistererWithPrefix("cortex_"+strconv.Itoa(i)+"_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
		miq.ring = append(miq.ring, r)

		logger := log.With(util_log.Logger, "component", "querier")
		ingesterQuerier, err := NewIngesterQuerier(querierCfg, clientCfg, r, nil, shardCountFun, namespace, logger)
		if err != nil {
			return nil, err
		}
		miq.ingesterQueriers = append(miq.ingesterQueriers, ingesterQuerier)
		srvc = append(srvc, r)
	}
	if miq.manager, err = services.NewManager(srvc...); err != nil {
		return nil, err
	}

	if err = services.StartManagerAndAwaitHealthy(context.Background(), miq.manager); err != nil {
		return nil, err
	}

	return miq, nil
}

func (miq *MultiIngesterQuerier) GetSubServices() *services.Manager {
	return miq.manager
}

func (miq *MultiIngesterQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	var (
		m    sync.Mutex
		iter []iter.EntryIterator
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].SelectLogs(ctx, params); err != nil {
				return err
			} else {
				m.Lock()
				iter = append(iter, i...)
				m.Unlock()
			}
			return nil
		})
	}
	return iter, g.Wait()
}

func (miq *MultiIngesterQuerier) SelectSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	var (
		m    sync.Mutex
		iter []iter.SampleIterator
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].SelectSample(ctx, params); err != nil {
				return err
			} else {
				m.Lock()
				iter = append(iter, i...)
				m.Unlock()
			}
			return nil
		})
	}
	return iter, g.Wait()
}

func (miq *MultiIngesterQuerier) Label(ctx context.Context, req *logproto.LabelRequest) ([][]string, error) {
	var (
		m    sync.Mutex
		iter [][]string
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].Label(ctx, req); err != nil {
				return err
			} else {
				m.Lock()
				iter = append(iter, i...)
				m.Unlock()
			}
			return nil
		})
	}
	return iter, g.Wait()
}

func (miq *MultiIngesterQuerier) Tail(ctx context.Context, req *logproto.TailRequest) (map[string]logproto.Querier_TailClient, error) {
	var (
		m           sync.Mutex
		tailClients = make(map[string]logproto.Querier_TailClient)
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].Tail(ctx, req); err != nil {
				return err
			} else {
				m.Lock()
				for k, v := range i {
					tailClients[k] = v
				}
				m.Unlock()
			}
			return nil
		})
	}
	return tailClients, g.Wait()
}

func (miq *MultiIngesterQuerier) TailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	var (
		m           sync.Mutex
		tailClients = make(map[string]logproto.Querier_TailClient)
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].TailDisconnectedIngesters(ctx, req, connectedIngestersAddr); err != nil {
				return err
			} else {
				m.Lock()
				for k, v := range i {
					tailClients[k] = v
				}
				m.Unlock()
			}
			return nil
		})
	}
	return tailClients, g.Wait()
}

func (miq *MultiIngesterQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) ([][]logproto.SeriesIdentifier, error) {
	var (
		m              sync.Mutex
		replicationSet [][]logproto.SeriesIdentifier
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].Series(ctx, req); err != nil {
				return err
			} else {
				m.Lock()
				replicationSet = append(replicationSet, i...)
				m.Unlock()
			}
			return nil
		})
	}
	return replicationSet, g.Wait()
}

func (miq *MultiIngesterQuerier) TailersCount(ctx context.Context) ([]uint32, error) {
	var (
		m      sync.Mutex
		counts []uint32
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].TailersCount(ctx); err != nil {
				return err
			} else {
				m.Lock()
				counts = append(counts, i...)
				m.Unlock()
			}
			return nil
		})
	}
	return counts, g.Wait()
}

func (miq *MultiIngesterQuerier) GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	var (
		m        sync.Mutex
		chunkIDs []string
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].GetChunkIDs(ctx, from, through, matchers...); err != nil {
				return err
			} else {
				m.Lock()
				chunkIDs = append(chunkIDs, i...)
				m.Unlock()
			}
			return nil
		})
	}
	return chunkIDs, g.Wait()
}

func (miq *MultiIngesterQuerier) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*index_stats.Stats, error) {
	var (
		m      sync.Mutex
		merged index_stats.Stats
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if resp, err := miq.ingesterQueriers[idx].Stats(ctx, userID, from, through, matchers...); err != nil {
				return err
			} else {
				m.Lock()
				merged = index_stats.MergeStats(resp, &merged)
				m.Unlock()
			}
			return nil
		})
	}
	return &merged, g.Wait()
}

func (miq *MultiIngesterQuerier) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	var (
		m      sync.Mutex
		merged *logproto.VolumeResponse
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if resp, err := miq.ingesterQueriers[idx].Volume(ctx, userID, from, through, limit, targetLabels, aggregateBy, matchers...); err != nil {
				return err
			} else {
				m.Lock()
				merged = seriesvolume.Merge([]*logproto.VolumeResponse{merged, resp}, limit)
				m.Unlock()
			}
			return nil
		})
	}
	return merged, g.Wait()
}

func (miq *MultiIngesterQuerier) DetectedLabel(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.LabelToValuesResponse, error) {
	var (
		m    sync.Mutex
		iter *logproto.LabelToValuesResponse
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		idx := idx
		g.Go(func() error {
			if i, err := miq.ingesterQueriers[idx].DetectedLabel(ctx, req); err != nil {
				return err
			} else {
				m.Lock()
				// TODO: implement MergeLabelToValuesResponse
				iter = i
				//iter = logproto.MergeLabelToValuesResponse(iter, i)
				m.Unlock()
			}
			return nil
		})
	}
	return iter, g.Wait()

}

func (miq *MultiIngesterQuerier) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var (
		mu  sync.Mutex
		rss ring.ReplicationSet
	)
	g, _ := errgroup.WithContext(req.Context())
	for _, r := range miq.ring {
		r := r
		g.Go(func() error {
			rs, err := r.GetAllHealthy(ring.Read)
			if err != nil {
				return err
			}
			mu.Lock()
			rss.Instances = append(rss.Instances, rs.Instances...)
			rss.MaxErrors += rs.MaxErrors
			rss.MaxUnavailableZones += rs.MaxUnavailableZones
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	body, _ := json.Marshal(rss)
	w.Write(body)
}
