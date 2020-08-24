// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/component"
	"github.com/thanos-io/thanos/pkg/promclient"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/thanos-io/thanos/pkg/strutil"
	"github.com/thanos-io/thanos/pkg/tracing"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ctxKey int

// StoreMatcherKey is the context key for the store's allow list.
const StoreMatcherKey = ctxKey(0)

// Client holds meta information about a store.
type Client interface {
	// Client to access the store.
	storepb.StoreClient

	// LabelSets that each apply to some data exposed by the backing store.
	LabelSets() []storepb.LabelSet

	// Minimum and maximum time range of data in the store.
	TimeRange() (mint int64, maxt int64)

	String() string
	// Addr returns address of a Client.
	Addr() string
}

// ProxyStore implements the store API that proxies request to all given underlying stores.
type ProxyStore struct {
	logger         log.Logger
	stores         func() []Client
	component      component.StoreAPI
	selectorLabels labels.Labels

	responseTimeout time.Duration
	metrics         *proxyStoreMetrics
}

type proxyStoreMetrics struct {
	emptyStreamResponses prometheus.Counter
}

func newProxyStoreMetrics(reg prometheus.Registerer) *proxyStoreMetrics {
	var m proxyStoreMetrics

	m.emptyStreamResponses = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "thanos_proxy_store_empty_stream_responses_total",
		Help: "Total number of empty responses received.",
	})

	return &m
}

// NewProxyStore returns a new ProxyStore that uses the given clients that implements storeAPI to fan-in all series to the client.
// Note that there is no deduplication support. Deduplication should be done on the highest level (just before PromQL).
func NewProxyStore(
	logger log.Logger,
	reg prometheus.Registerer,
	stores func() []Client,
	component component.StoreAPI,
	selectorLabels labels.Labels,
	responseTimeout time.Duration,
) *ProxyStore {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	metrics := newProxyStoreMetrics(reg)
	s := &ProxyStore{
		logger:          logger,
		stores:          stores,
		component:       component,
		selectorLabels:  selectorLabels,
		responseTimeout: responseTimeout,
		metrics:         metrics,
	}
	return s
}

// Info returns store information about the external labels this store have.
func (s *ProxyStore) Info(_ context.Context, _ *storepb.InfoRequest) (*storepb.InfoResponse, error) {
	res := &storepb.InfoResponse{
		Labels:    make([]storepb.Label, 0, len(s.selectorLabels)),
		StoreType: s.component.ToProto(),
	}

	minTime := int64(math.MaxInt64)
	maxTime := int64(0)
	stores := s.stores()

	// Edge case: we have no data if there are no stores.
	if len(stores) == 0 {
		res.MaxTime = 0
		res.MinTime = 0

		return res, nil
	}

	for _, s := range stores {
		mint, maxt := s.TimeRange()
		if mint < minTime {
			minTime = mint
		}
		if maxt > maxTime {
			maxTime = maxt
		}
	}

	res.MaxTime = maxTime
	res.MinTime = minTime

	for _, l := range s.selectorLabels {
		res.Labels = append(res.Labels, storepb.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}

	labelSets := make(map[uint64][]storepb.Label, len(stores))
	for _, st := range stores {
		for _, labelSet := range st.LabelSets() {
			mergedLabelSet := mergeLabels(labelSet.Labels, s.selectorLabels)
			ls := storepb.LabelsToPromLabels(mergedLabelSet)
			sort.Sort(ls)
			labelSets[ls.Hash()] = mergedLabelSet
		}
	}

	res.LabelSets = make([]storepb.LabelSet, 0, len(labelSets))
	for _, v := range labelSets {
		res.LabelSets = append(res.LabelSets, storepb.LabelSet{Labels: v})
	}

	// We always want to enforce announcing the subset of data that
	// selector-labels represents. If no label-sets are announced by the
	// store-proxy's discovered stores, then we still want to enforce
	// announcing this subset by announcing the selector as the label-set.
	if len(res.LabelSets) == 0 && len(res.Labels) > 0 {
		res.LabelSets = append(res.LabelSets, storepb.LabelSet{Labels: res.Labels})
	}

	return res, nil
}

// mergeLabels merges label-set a and label-selector b with the selector's
// labels having precedence. The types are distinct because of the inputs at
// hand where this function is used.
func mergeLabels(a []storepb.Label, b labels.Labels) []storepb.Label {
	ls := map[string]string{}
	for _, l := range a {
		ls[l.Name] = l.Value
	}
	for _, l := range b {
		ls[l.Name] = l.Value
	}

	res := []storepb.Label{}
	for k, v := range ls {
		res = append(res, storepb.Label{Name: k, Value: v})
	}

	return res
}

// cancelableRespSender is a response channel that does need to be exhausted on cancel.
type cancelableRespSender struct {
	ctx context.Context
	ch  chan<- *storepb.SeriesResponse
}

func newCancelableRespChannel(ctx context.Context, buffer int) (*cancelableRespSender, chan *storepb.SeriesResponse) {
	respCh := make(chan *storepb.SeriesResponse, buffer)
	return &cancelableRespSender{ctx: ctx, ch: respCh}, respCh
}

// send or return on cancel.
func (s cancelableRespSender) send(r *storepb.SeriesResponse) {
	select {
	case <-s.ctx.Done():
	case s.ch <- r:
	}
}

// Series returns all series for a requested time range and label matcher. Requested series are taken from other
// stores and proxied to RPC client. NOTE: Resulted data are not trimmed exactly to min and max time range.
func (s *ProxyStore) Series(r *storepb.SeriesRequest, srv storepb.Store_SeriesServer) error {
	match, newMatchers, err := matchesExternalLabels(r.Matchers, s.selectorLabels)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if !match {
		return nil
	}

	if len(newMatchers) == 0 {
		return status.Error(codes.InvalidArgument, errors.New("no matchers specified (excluding external labels)").Error())
	}

	g, gctx := errgroup.WithContext(srv.Context())

	// Allow to buffer max 10 series response.
	// Each might be quite large (multi chunk long series given by sidecar).
	respSender, respCh := newCancelableRespChannel(gctx, 10)

	g.Go(func() error {
		// This go routine is responsible for calling store's Series concurrently. Merged results
		// are passed to respCh and sent concurrently to client (if buffer of 10 have room).
		// When this go routine finishes or is canceled, respCh channel is closed.

		var (
			seriesSet      []storepb.SeriesSet
			storeDebugMsgs []string
			r              = &storepb.SeriesRequest{
				MinTime:                 r.MinTime,
				MaxTime:                 r.MaxTime,
				Matchers:                newMatchers,
				Aggregates:              r.Aggregates,
				MaxResolutionWindow:     r.MaxResolutionWindow,
				SkipChunks:              r.SkipChunks,
				PartialResponseDisabled: r.PartialResponseDisabled,
			}
			wg = &sync.WaitGroup{}
		)

		defer func() {
			wg.Wait()
			close(respCh)
		}()

		for _, st := range s.stores() {
			// We might be able to skip the store if its meta information indicates
			// it cannot have series matching our query.
			// NOTE: all matchers are validated in matchesExternalLabels method so we explicitly ignore error.
			var ok bool
			tracing.DoInSpan(gctx, "store_matches", func(ctx context.Context) {
				storeMatcher := [][]storepb.LabelMatcher{}
				if ctxVal := srv.Context().Value(StoreMatcherKey); ctxVal != nil {
					if value, ok := ctxVal.([][]storepb.LabelMatcher); ok {
						storeMatcher = value
					}
				}
				// We can skip error, we already translated matchers once.
				ok, _ = storeMatches(st, r.MinTime, r.MaxTime, storeMatcher, r.Matchers...)
			})
			if !ok {
				storeDebugMsgs = append(storeDebugMsgs, fmt.Sprintf("store %s filtered out", st))
				continue
			}
			storeDebugMsgs = append(storeDebugMsgs, fmt.Sprintf("store %s queried", st))

			// This is used to cancel this stream when one operations takes too long.
			seriesCtx, closeSeries := context.WithCancel(gctx)
			seriesCtx = grpc_opentracing.ClientAddContextTags(seriesCtx, opentracing.Tags{
				"target": st.Addr(),
			})
			defer closeSeries()

			sc, err := st.Series(seriesCtx, r)
			if err != nil {
				storeID := storepb.LabelSetsToString(st.LabelSets())
				if storeID == "" {
					storeID = "Store Gateway"
				}
				err = errors.Wrapf(err, "fetch series for %s %s", storeID, st)
				if r.PartialResponseDisabled {
					level.Error(s.logger).Log("err", err, "msg", "partial response disabled; aborting request")
					return err
				}
				respSender.send(storepb.NewWarnSeriesResponse(err))
				continue
			}

			// Schedule streamSeriesSet that translates gRPC streamed response
			// into seriesSet (if series) or respCh if warnings.
			seriesSet = append(seriesSet, startStreamSeriesSet(seriesCtx, s.logger, closeSeries,
				wg, sc, respSender, st.String(), !r.PartialResponseDisabled, s.responseTimeout, s.metrics.emptyStreamResponses))
		}

		level.Debug(s.logger).Log("msg", strings.Join(storeDebugMsgs, ";"))
		if len(seriesSet) == 0 {
			// This is indicates that configured StoreAPIs are not the ones end user expects.
			err := errors.New("No StoreAPIs matched for this query")
			level.Warn(s.logger).Log("err", err, "stores", strings.Join(storeDebugMsgs, ";"))
			respSender.send(storepb.NewWarnSeriesResponse(err))
			return nil
		}

		// TODO(bwplotka): Currently we stream into big frames. Consider ensuring 1MB maximum.
		// This however does not matter much when used with QueryAPI. Matters for federated Queries a lot.
		// https://github.com/thanos-io/thanos/issues/2332
		// Series are not necessarily merged across themselves.
		mergedSet := storepb.MergeSeriesSets(seriesSet...)
		for mergedSet.Next() {
			var series storepb.Series
			series.Labels, series.Chunks = mergedSet.At()
			respSender.send(storepb.NewSeriesResponse(&series))
		}
		return mergedSet.Err()
	})
	g.Go(func() error {
		// Go routine for gathering merged responses and sending them over to client. It stops when
		// respCh channel is closed OR on error from client.
		for resp := range respCh {
			if err := srv.Send(resp); err != nil {
				return status.Error(codes.Unknown, errors.Wrap(err, "send series response").Error())
			}
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		// TODO(bwplotka): Replace with request logger.
		level.Error(s.logger).Log("err", err)
		return err
	}
	return nil
}

type directSender interface {
	send(*storepb.SeriesResponse)
}

// streamSeriesSet iterates over incoming stream of series.
// All errors are sent out of band via warning channel.
type streamSeriesSet struct {
	ctx    context.Context
	logger log.Logger

	stream storepb.Store_SeriesClient
	warnCh directSender

	currSeries *storepb.Series
	recvCh     chan *storepb.Series

	errMtx sync.Mutex
	err    error

	name            string
	partialResponse bool

	responseTimeout time.Duration
	closeSeries     context.CancelFunc
}

type recvResponse struct {
	r   *storepb.SeriesResponse
	err error
}

func frameCtx(responseTimeout time.Duration) (context.Context, context.CancelFunc) {
	frameTimeoutCtx := context.Background()
	var cancel context.CancelFunc
	if responseTimeout != 0 {
		frameTimeoutCtx, cancel = context.WithTimeout(frameTimeoutCtx, responseTimeout)
		return frameTimeoutCtx, cancel
	}
	return frameTimeoutCtx, func() {}
}

func startStreamSeriesSet(
	ctx context.Context,
	logger log.Logger,
	closeSeries context.CancelFunc,
	wg *sync.WaitGroup,
	stream storepb.Store_SeriesClient,
	warnCh directSender,
	name string,
	partialResponse bool,
	responseTimeout time.Duration,
	emptyStreamResponses prometheus.Counter,
) *streamSeriesSet {
	s := &streamSeriesSet{
		ctx:             ctx,
		logger:          logger,
		closeSeries:     closeSeries,
		stream:          stream,
		warnCh:          warnCh,
		recvCh:          make(chan *storepb.Series, 10),
		name:            name,
		partialResponse: partialResponse,
		responseTimeout: responseTimeout,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(s.recvCh)

		numResponses := 0
		defer func() {
			if numResponses == 0 {
				emptyStreamResponses.Inc()
			}
		}()

		rCh := make(chan *recvResponse)
		done := make(chan struct{})
		go func() {
			for {
				r, err := s.stream.Recv()
				select {
				case <-done:
					close(rCh)
					return
				case rCh <- &recvResponse{r: r, err: err}:
				}
			}
		}()
		for {
			frameTimeoutCtx, cancel := frameCtx(s.responseTimeout)
			defer cancel()
			var rr *recvResponse
			select {
			case <-ctx.Done():
				s.handleErr(errors.Wrapf(ctx.Err(), "failed to receive any data from %s", s.name), done)
				return
			case <-frameTimeoutCtx.Done():
				s.handleErr(errors.Wrapf(frameTimeoutCtx.Err(), "failed to receive any data in %s from %s", s.responseTimeout.String(), s.name), done)
				return
			case rr = <-rCh:
			}

			if rr.err == io.EOF {
				close(done)
				return
			}

			if rr.err != nil {
				s.handleErr(errors.Wrapf(rr.err, "receive series from %s", s.name), done)
				return
			}
			numResponses++

			if w := rr.r.GetWarning(); w != "" {
				s.warnCh.send(storepb.NewWarnSeriesResponse(errors.New(w)))
			}

			if series := rr.r.GetSeries(); series != nil {
				select {
				case s.recvCh <- series:
				case <-ctx.Done():
					s.handleErr(errors.Wrapf(ctx.Err(), "failed to receive any data from %s", s.name), done)
					return
				}
			}
		}
	}()
	return s
}

func (s *streamSeriesSet) handleErr(err error, done chan struct{}) {
	defer close(done)
	s.closeSeries()

	if s.partialResponse {
		level.Warn(s.logger).Log("err", err, "msg", "returning partial response")
		s.warnCh.send(storepb.NewWarnSeriesResponse(err))
		return
	}
	s.errMtx.Lock()
	s.err = err
	s.errMtx.Unlock()
}

// Next blocks until new message is received or stream is closed or operation is timed out.
func (s *streamSeriesSet) Next() (ok bool) {
	s.currSeries, ok = <-s.recvCh
	return ok
}

func (s *streamSeriesSet) At() ([]storepb.Label, []storepb.AggrChunk) {
	if s.currSeries == nil {
		return nil, nil
	}
	return s.currSeries.Labels, s.currSeries.Chunks
}

func (s *streamSeriesSet) Err() error {
	s.errMtx.Lock()
	defer s.errMtx.Unlock()
	return errors.Wrap(s.err, s.name)
}

// matchStore returns true if the given store may hold data for the given label
// matchers.
func storeMatches(s Client, mint, maxt int64, storeMatcher [][]storepb.LabelMatcher, matchers ...storepb.LabelMatcher) (bool, error) {
	storeMinTime, storeMaxTime := s.TimeRange()
	if mint > storeMaxTime || maxt < storeMinTime {
		return false, nil
	}
	match, err := storeMatchMetadata(s, storeMatcher)
	if err != nil || !match {
		return match, err
	}
	return labelSetsMatch(s.LabelSets(), matchers)
}

// storeMatch return true if the store's metadata labels match the storeMatcher.
func storeMatchMetadata(s Client, storeMatcher [][]storepb.LabelMatcher) (bool, error) {
	clientLabels := generateMetadataClientLabels(s)
	if len(storeMatcher) == 0 {
		return true, nil
	}
	res := false
	for _, stm := range storeMatcher {
		stmMatch, err := labelSetMatches(clientLabels, stm)
		if err != nil {
			return false, err
		}
		res = res || stmMatch
	}
	return res, nil
}

func generateMetadataClientLabels(s Client) storepb.LabelSet {
	l := storepb.Label{Name: "__address__", Value: s.Addr()}
	return storepb.LabelSet{Labels: []storepb.Label{l}}
}

// labelSetsMatch returns false if all label-set do not match the matchers.
func labelSetsMatch(lss []storepb.LabelSet, matchers []storepb.LabelMatcher) (bool, error) {
	if len(lss) == 0 {
		return true, nil
	}

	res := false
	for _, ls := range lss {
		lsMatch, err := labelSetMatches(ls, matchers)
		if err != nil {
			return false, err
		}
		res = res || lsMatch
	}
	return res, nil
}

// labelSetMatches returns false if any matcher matches negatively against the
// respective label-value for the matcher's label-name.
func labelSetMatches(ls storepb.LabelSet, matchers []storepb.LabelMatcher) (bool, error) {
	for _, m := range matchers {
		for _, l := range ls.Labels {
			if l.Name != m.Name {
				continue
			}

			m, err := promclient.TranslateMatcher(m)
			if err != nil {
				return false, err
			}

			if !m.Matches(l.Value) {
				return false, nil
			}
		}
	}
	return true, nil
}

// LabelNames returns all known label names.
func (s *ProxyStore) LabelNames(ctx context.Context, r *storepb.LabelNamesRequest) (
	*storepb.LabelNamesResponse, error,
) {
	var (
		warnings []string
		names    [][]string
		mtx      sync.Mutex
		g, gctx  = errgroup.WithContext(ctx)
	)

	for _, st := range s.stores() {
		st := st
		g.Go(func() error {
			resp, err := st.LabelNames(gctx, &storepb.LabelNamesRequest{
				PartialResponseDisabled: r.PartialResponseDisabled,
				Start:                   r.Start,
				End:                     r.End,
			})
			if err != nil {
				err = errors.Wrapf(err, "fetch label names from store %s", st)
				if r.PartialResponseDisabled {
					return err
				}

				mtx.Lock()
				warnings = append(warnings, err.Error())
				mtx.Unlock()
				return nil
			}

			mtx.Lock()
			warnings = append(warnings, resp.Warnings...)
			names = append(names, resp.Names)
			mtx.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &storepb.LabelNamesResponse{
		Names:    strutil.MergeUnsortedSlices(names...),
		Warnings: warnings,
	}, nil
}

// LabelValues returns all known label values for a given label name.
func (s *ProxyStore) LabelValues(ctx context.Context, r *storepb.LabelValuesRequest) (
	*storepb.LabelValuesResponse, error,
) {
	var (
		warnings []string
		all      [][]string
		mtx      sync.Mutex
		g, gctx  = errgroup.WithContext(ctx)
	)

	for _, st := range s.stores() {
		store := st
		g.Go(func() error {
			resp, err := store.LabelValues(gctx, &storepb.LabelValuesRequest{
				Label:                   r.Label,
				PartialResponseDisabled: r.PartialResponseDisabled,
				Start:                   r.Start,
				End:                     r.End,
			})
			if err != nil {
				err = errors.Wrapf(err, "fetch label values from store %s", store)
				if r.PartialResponseDisabled {
					return err
				}

				mtx.Lock()
				warnings = append(warnings, errors.Wrap(err, "fetch label values").Error())
				mtx.Unlock()
				return nil
			}

			mtx.Lock()
			warnings = append(warnings, resp.Warnings...)
			all = append(all, resp.Values)
			mtx.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &storepb.LabelValuesResponse{
		Values:   strutil.MergeUnsortedSlices(all...),
		Warnings: warnings,
	}, nil
}
