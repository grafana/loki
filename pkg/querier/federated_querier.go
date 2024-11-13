package querier

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	index_stats "github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type FederatedQueryConfig struct {
	ClusterRings []ring.LifecyclerConfig `yaml:"cluster_rings,omitempty"`
}

type FederatedQuerier struct {
	services.Service
	cfg              *FederatedQueryConfig
	manager          *services.Manager
	ring             []*ring.Ring
	ingesterQueriers []storage.IIngesterQuerier
}

func NewFederatedQuerier(cfg FederatedQueryConfig, clientCfg client.Config, querierCfg Config, shardCountFun func(userID string) int, namespace string) (storage.IIngesterQuerier, error) {
	var (
		err  error
		srvc = make([]services.Service, 0, len(cfg.ClusterRings))
		miq  = &FederatedQuerier{
			cfg:              &cfg,
			ring:             make([]*ring.Ring, 0, len(cfg.ClusterRings)),
			ingesterQueriers: make([]storage.IIngesterQuerier, 0, len(cfg.ClusterRings)),
		}
	)

	for i, c := range cfg.ClusterRings {
		r, err := ring.New(c.RingConfig, "ingester", ingester.RingKey, util_log.Logger, prometheus.WrapRegistererWithPrefix("cortex_"+strconv.Itoa(i)+"_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
		miq.ring = append(miq.ring, r)

		logger := log.With(util_log.Logger, "component", "querier")

		// TODO(honganan): support kafka partition ingestion
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

func (miq *FederatedQuerier) GetSubServices() *services.Manager {
	return miq.manager
}

func (miq *FederatedQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	var (
		m     sync.Mutex
		iters []iter.EntryIterator
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].SelectLogs(ctx, params)
			if err != nil {
				return err
			}
			m.Lock()
			iters = append(iters, i...)
			m.Unlock()
			return nil
		})
	}
	return iters, g.Wait()
}

func (miq *FederatedQuerier) SelectSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	var (
		m     sync.Mutex
		iters []iter.SampleIterator
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].SelectSample(ctx, params)
			if err != nil {
				return err
			}
			m.Lock()
			iters = append(iters, i...)
			m.Unlock()
			return nil
		})
	}
	return iters, g.Wait()
}

func (miq *FederatedQuerier) Label(ctx context.Context, req *logproto.LabelRequest) ([][]string, error) {
	var (
		m   sync.Mutex
		lbs [][]string
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].Label(ctx, req)
			if err != nil {
				return err
			}
			m.Lock()
			lbs = append(lbs, i...)
			m.Unlock()
			return nil
		})
	}
	return lbs, g.Wait()
}

func (miq *FederatedQuerier) Tail(ctx context.Context, req *logproto.TailRequest) (map[string]logproto.Querier_TailClient, error) {
	var (
		m           sync.Mutex
		tailClients = make(map[string]logproto.Querier_TailClient)
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].Tail(ctx, req)
			if err != nil {
				return err
			}
			m.Lock()
			for k, v := range i {
				tailClients[k] = v
			}
			m.Unlock()
			return nil
		})
	}
	return tailClients, g.Wait()
}

func (miq *FederatedQuerier) TailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	var (
		m           sync.Mutex
		tailClients = make(map[string]logproto.Querier_TailClient)
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].TailDisconnectedIngesters(ctx, req, connectedIngestersAddr)
			if err != nil {
				return err
			}
			m.Lock()
			for k, v := range i {
				tailClients[k] = v
			}
			m.Unlock()
			return nil
		})
	}
	return tailClients, g.Wait()
}

func (miq *FederatedQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) ([][]logproto.SeriesIdentifier, error) {
	var (
		m              sync.Mutex
		replicationSet [][]logproto.SeriesIdentifier
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].Series(ctx, req)
			if err != nil {
				return err
			}
			m.Lock()
			replicationSet = append(replicationSet, i...)
			m.Unlock()
			return nil
		})
	}
	return replicationSet, g.Wait()
}

func (miq *FederatedQuerier) TailersCount(ctx context.Context) ([]uint32, error) {
	var (
		m      sync.Mutex
		counts []uint32
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].TailersCount(ctx)
			if err != nil {
				return err
			}
			m.Lock()
			counts = append(counts, i...)
			m.Unlock()
			return nil
		})
	}
	return counts, g.Wait()
}

func (miq *FederatedQuerier) GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	var (
		m        sync.Mutex
		chunkIDs []string
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].GetChunkIDs(ctx, from, through, matchers...)
			if err != nil {
				return err
			}
			m.Lock()
			chunkIDs = append(chunkIDs, i...)
			m.Unlock()
			return nil
		})
	}
	return chunkIDs, g.Wait()
}

func (miq *FederatedQuerier) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*index_stats.Stats, error) {
	var (
		m      sync.Mutex
		merged index_stats.Stats
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			resp, err := miq.ingesterQueriers[idx].Stats(ctx, userID, from, through, matchers...)
			if err != nil {
				return err
			}
			m.Lock()
			merged = index_stats.MergeStats(resp, &merged)
			m.Unlock()
			return nil
		})
	}
	return &merged, g.Wait()
}

func (miq *FederatedQuerier) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	var (
		m      sync.Mutex
		merged *logproto.VolumeResponse
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			resp, err := miq.ingesterQueriers[idx].Volume(ctx, userID, from, through, limit, targetLabels, aggregateBy, matchers...)
			if err != nil {
				return err
			}
			m.Lock()
			merged = seriesvolume.Merge([]*logproto.VolumeResponse{merged, resp}, limit)
			m.Unlock()
			return nil
		})
	}
	return merged, g.Wait()
}

func (miq *FederatedQuerier) DetectedLabel(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.LabelToValuesResponse, error) {
	var (
		m   sync.Mutex
		rsp *logproto.LabelToValuesResponse
	)
	g, _ := errgroup.WithContext(ctx)
	for idx := range miq.ingesterQueriers {
		g.Go(func() error {
			i, err := miq.ingesterQueriers[idx].DetectedLabel(ctx, req)
			if err != nil {
				return err
			}
			m.Lock()
			rsp = miq.mergeLabelToValuesResponse(rsp, i)
			m.Unlock()
			return nil
		})
	}
	return rsp, g.Wait()

}

func (miq *FederatedQuerier) mergeLabelToValuesResponse(a, b *logproto.LabelToValuesResponse) *logproto.LabelToValuesResponse {
	labelMap := make(map[string][]string)
	for _, rsp := range []*logproto.LabelToValuesResponse{a, b} {
		if rsp == nil {
			continue
		}

		for label, values := range rsp.Labels {
			var combinedValues []string
			allValues, isLabelPresent := labelMap[label]
			if isLabelPresent {
				combinedValues = append(allValues, values.Values...)
			} else {
				combinedValues = values.Values
			}
			labelMap[label] = combinedValues
		}
	}

	// Dedupe all ingester values
	mergedResult := make(map[string]*logproto.UniqueLabelValues)
	for label, val := range labelMap {
		slices.Sort(val)
		uniqueValues := slices.Compact(val)

		mergedResult[label] = &logproto.UniqueLabelValues{
			Values: uniqueValues,
		}
	}
	return &logproto.LabelToValuesResponse{Labels: mergedResult}
}

func (miq *FederatedQuerier) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var (
		mu  sync.Mutex
		rss ring.ReplicationSet
	)
	g, _ := errgroup.WithContext(req.Context())
	for _, r := range miq.ring {
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
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			_ = level.Error(util_log.Logger).Log("msg", "failed to write response", "err", err)
		}
		return
	}
	body, _ := json.Marshal(rss)
	_, err := w.Write(body)
	if err != nil {
		_ = level.Error(util_log.Logger).Log("msg", "failed to write response", "err", err)
	}
}
