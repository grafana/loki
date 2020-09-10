package distributor

import (
	"context"
	"flag"
	"net/http"
	"sort"
	"strings"
	"time"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	ingester_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/prom1/storage/metric"
	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/extract"
	"github.com/cortexproject/cortex/pkg/util/limiter"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

var (
	queryDuration = instrument.NewHistogramCollector(promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "distributor_query_duration_seconds",
		Help:      "Time spent executing expression queries.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30},
	}, []string{"method", "status_code"}))
	receivedSamples = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_received_samples_total",
		Help:      "The total number of received samples, excluding rejected and deduped samples.",
	}, []string{"user"})
	receivedMetadata = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_received_metadata_total",
		Help:      "The total number of received metadata, excluding rejected.",
	}, []string{"user"})
	incomingSamples = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_samples_in_total",
		Help:      "The total number of samples that have come in to the distributor, including rejected or deduped samples.",
	}, []string{"user"})
	incomingMetadata = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_metadata_in_total",
		Help:      "The total number of metadata the have come in to the distributor, including rejected.",
	}, []string{"user"})
	nonHASamples = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_non_ha_samples_received_total",
		Help:      "The total number of received samples for a user that has HA tracking turned on, but the sample didn't contain both HA labels.",
	}, []string{"user"})
	dedupedSamples = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_deduped_samples_total",
		Help:      "The total number of deduplicated samples.",
	}, []string{"user", "cluster"})
	labelsHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "labels_per_sample",
		Help:      "Number of labels per sample.",
		Buckets:   []float64{5, 10, 15, 20, 25},
	})
	ingesterAppends = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_ingester_appends_total",
		Help:      "The total number of batch appends sent to ingesters.",
	}, []string{"ingester", "type"})
	ingesterAppendFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_ingester_append_failures_total",
		Help:      "The total number of failed batch appends sent to ingesters.",
	}, []string{"ingester", "type"})
	ingesterQueries = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_ingester_queries_total",
		Help:      "The total number of queries sent to ingesters.",
	}, []string{"ingester"})
	ingesterQueryFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_ingester_query_failures_total",
		Help:      "The total number of failed queries sent to ingesters.",
	}, []string{"ingester"})
	replicationFactor = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "distributor_replication_factor",
		Help:      "The configured replication factor.",
	})
	latestSeenSampleTimestampPerUser = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cortex_distributor_latest_seen_sample_timestamp_seconds",
		Help: "Unix timestamp of latest received sample per user.",
	}, []string{"user"})
	emptyPreallocSeries = ingester_client.PreallocTimeseries{}
)

const (
	typeSamples  = "samples"
	typeMetadata = "metadata"
)

// Distributor is a storage.SampleAppender and a client.Querier which
// forwards appends and queries to individual ingesters.
type Distributor struct {
	services.Service

	cfg           Config
	ingestersRing ring.ReadRing
	ingesterPool  *ring_client.Pool
	limits        *validation.Overrides

	// The global rate limiter requires a distributors ring to count
	// the number of healthy instances
	distributorsRing *ring.Lifecycler

	// For handling HA replicas.
	HATracker *haTracker

	// Per-user rate limiter.
	ingestionRateLimiter *limiter.RateLimiter

	// Manager for subservices (HA Tracker, distributor ring and client pool)
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

// Config contains the configuration require to
// create a Distributor
type Config struct {
	PoolConfig PoolConfig `yaml:"pool"`

	HATrackerConfig HATrackerConfig `yaml:"ha_tracker"`

	MaxRecvMsgSize  int           `yaml:"max_recv_msg_size"`
	RemoteTimeout   time.Duration `yaml:"remote_timeout"`
	ExtraQueryDelay time.Duration `yaml:"extra_queue_delay"`

	ShardByAllLabels bool `yaml:"shard_by_all_labels"`

	// Distributors ring
	DistributorRing RingConfig `yaml:"ring"`

	// for testing
	ingesterClientFactory ring_client.PoolFactory `yaml:"-"`

	// when true the distributor does not validate the label name, Cortex doesn't directly use
	// this (and should never use it) but this feature is used by other projects built on top of it
	SkipLabelNameValidation bool `yaml:"-"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.PoolConfig.RegisterFlags(f)
	cfg.HATrackerConfig.RegisterFlags(f)
	cfg.DistributorRing.RegisterFlags(f)

	f.IntVar(&cfg.MaxRecvMsgSize, "distributor.max-recv-msg-size", 100<<20, "remote_write API max receive message size (bytes).")
	f.DurationVar(&cfg.RemoteTimeout, "distributor.remote-timeout", 2*time.Second, "Timeout for downstream ingesters.")
	f.DurationVar(&cfg.ExtraQueryDelay, "distributor.extra-query-delay", 0, "Time to wait before sending more than the minimum successful query requests.")
	f.BoolVar(&cfg.ShardByAllLabels, "distributor.shard-by-all-labels", false, "Distribute samples based on all labels, as opposed to solely by user and metric name.")
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	return cfg.HATrackerConfig.Validate()
}

// New constructs a new Distributor
func New(cfg Config, clientConfig ingester_client.Config, limits *validation.Overrides, ingestersRing ring.ReadRing, canJoinDistributorsRing bool, reg prometheus.Registerer) (*Distributor, error) {
	if cfg.ingesterClientFactory == nil {
		cfg.ingesterClientFactory = func(addr string) (ring_client.PoolClient, error) {
			return ingester_client.MakeIngesterClient(addr, clientConfig)
		}
	}

	replicationFactor.Set(float64(ingestersRing.ReplicationFactor()))
	cfg.PoolConfig.RemoteTimeout = cfg.RemoteTimeout

	replicas, err := newClusterTracker(cfg.HATrackerConfig, reg)
	if err != nil {
		return nil, err
	}

	subservices := []services.Service(nil)
	subservices = append(subservices, replicas)

	// Create the configured ingestion rate limit strategy (local or global). In case
	// it's an internal dependency and can't join the distributors ring, we skip rate
	// limiting.
	var ingestionRateStrategy limiter.RateLimiterStrategy
	var distributorsRing *ring.Lifecycler

	if !canJoinDistributorsRing {
		ingestionRateStrategy = newInfiniteIngestionRateStrategy()
	} else if limits.IngestionRateStrategy() == validation.GlobalIngestionRateStrategy {
		distributorsRing, err = ring.NewLifecycler(cfg.DistributorRing.ToLifecyclerConfig(), nil, "distributor", ring.DistributorRingKey, true, reg)
		if err != nil {
			return nil, err
		}

		subservices = append(subservices, distributorsRing)

		ingestionRateStrategy = newGlobalIngestionRateStrategy(limits, distributorsRing)
	} else {
		ingestionRateStrategy = newLocalIngestionRateStrategy(limits)
	}

	d := &Distributor{
		cfg:                  cfg,
		ingestersRing:        ingestersRing,
		ingesterPool:         NewPool(cfg.PoolConfig, ingestersRing, cfg.ingesterClientFactory, util.Logger),
		distributorsRing:     distributorsRing,
		limits:               limits,
		ingestionRateLimiter: limiter.NewRateLimiter(ingestionRateStrategy, 10*time.Second),
		HATracker:            replicas,
	}

	subservices = append(subservices, d.ingesterPool)
	d.subservices, err = services.NewManager(subservices...)
	if err != nil {
		return nil, err
	}
	d.subservicesWatcher = services.NewFailureWatcher()
	d.subservicesWatcher.WatchManager(d.subservices)

	d.Service = services.NewBasicService(d.starting, d.running, d.stopping)
	return d, nil
}

func (d *Distributor) starting(ctx context.Context) error {
	// Only report success if all sub-services start properly
	return services.StartManagerAndAwaitHealthy(ctx, d.subservices)
}

func (d *Distributor) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-d.subservicesWatcher.Chan():
		return errors.Wrap(err, "distributor subservice failed")
	}
}

// Called after distributor is asked to stop via StopAsync.
func (d *Distributor) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), d.subservices)
}

func (d *Distributor) tokenForLabels(userID string, labels []client.LabelAdapter) (uint32, error) {
	if d.cfg.ShardByAllLabels {
		return shardByAllLabels(userID, labels), nil
	}

	metricName, err := extract.MetricNameFromLabelAdapters(labels)
	if err != nil {
		return 0, err
	}
	return shardByMetricName(userID, metricName), nil
}

func (d *Distributor) tokenForMetadata(userID string, metricName string) uint32 {
	if d.cfg.ShardByAllLabels {
		return shardByMetricName(userID, metricName)
	}

	return shardByUser(userID)
}

func shardByMetricName(userID string, metricName string) uint32 {
	h := shardByUser(userID)
	h = client.HashAdd32(h, metricName)
	return h
}

func shardByUser(userID string) uint32 {
	h := client.HashNew32()
	h = client.HashAdd32(h, userID)
	return h
}

// This function generates different values for different order of same labels.
func shardByAllLabels(userID string, labels []client.LabelAdapter) uint32 {
	h := shardByUser(userID)
	for _, label := range labels {
		h = client.HashAdd32(h, label.Name)
		h = client.HashAdd32(h, label.Value)
	}
	return h
}

// Remove the label labelname from a slice of LabelPairs if it exists.
func removeLabel(labelName string, labels *[]client.LabelAdapter) {
	for i := 0; i < len(*labels); i++ {
		pair := (*labels)[i]
		if pair.Name == labelName {
			*labels = append((*labels)[:i], (*labels)[i+1:]...)
			return
		}
	}
}

// Returns a boolean that indicates whether or not we want to remove the replica label going forward,
// and an error that indicates whether we want to accept samples based on the cluster/replica found in ts.
// nil for the error means accept the sample.
func (d *Distributor) checkSample(ctx context.Context, userID, cluster, replica string) (bool, error) {
	// If the sample doesn't have either HA label, accept it.
	// At the moment we want to accept these samples by default.
	if cluster == "" || replica == "" {
		return false, nil
	}

	// At this point we know we have both HA labels, we should lookup
	// the cluster/instance here to see if we want to accept this sample.
	err := d.HATracker.checkReplica(ctx, userID, cluster, replica)
	// checkReplica should only have returned an error if there was a real error talking to Consul, or if the replica labels don't match.
	if err != nil { // Don't accept the sample.
		return false, err
	}
	return true, nil
}

// Validates a single series from a write request. Will remove labels if
// any are configured to be dropped for the user ID.
// Returns the validated series with it's labels/samples, and any error.
func (d *Distributor) validateSeries(ts ingester_client.PreallocTimeseries, userID string) (client.PreallocTimeseries, error) {
	labelsHistogram.Observe(float64(len(ts.Labels)))
	if err := validation.ValidateLabels(d.limits, userID, ts.Labels, d.cfg.SkipLabelNameValidation); err != nil {
		return emptyPreallocSeries, err
	}

	metricName, _ := extract.MetricNameFromLabelAdapters(ts.Labels)
	samples := make([]client.Sample, 0, len(ts.Samples))
	for _, s := range ts.Samples {
		if err := validation.ValidateSample(d.limits, userID, metricName, s); err != nil {
			return emptyPreallocSeries, err
		}
		samples = append(samples, s)
	}

	return client.PreallocTimeseries{
			TimeSeries: &client.TimeSeries{
				Labels:  ts.Labels,
				Samples: samples,
			},
		},
		nil
}

// Push implements client.IngesterServer
func (d *Distributor) Push(ctx context.Context, req *client.WriteRequest) (*client.WriteResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}
	source := util.GetSourceIPsFromOutgoingCtx(ctx)

	var firstPartialErr error
	removeReplica := false

	numSamples := 0
	for _, ts := range req.Timeseries {
		numSamples += len(ts.Samples)
	}
	// Count the total samples in, prior to validation or deduplication, for comparison with other metrics.
	incomingSamples.WithLabelValues(userID).Add(float64(numSamples))
	// Count the total number of metadata in.
	incomingMetadata.WithLabelValues(userID).Add(float64(len(req.Metadata)))

	// A WriteRequest can only contain series or metadata but not both. This might change in the future.
	// For each timeseries or samples, we compute a hash to distribute across ingesters;
	// check each sample/metadata and discard if outside limits.
	validatedTimeseries := make([]client.PreallocTimeseries, 0, len(req.Timeseries))
	validatedMetadata := make([]*client.MetricMetadata, 0, len(req.Metadata))
	metadataKeys := make([]uint32, 0, len(req.Metadata))
	seriesKeys := make([]uint32, 0, len(req.Timeseries))
	validatedSamples := 0

	if d.limits.AcceptHASamples(userID) && len(req.Timeseries) > 0 {
		cluster, replica := findHALabels(d.limits.HAReplicaLabel(userID), d.limits.HAClusterLabel(userID), req.Timeseries[0].Labels)
		removeReplica, err = d.checkSample(ctx, userID, cluster, replica)
		if err != nil {
			if resp, ok := httpgrpc.HTTPResponseFromError(err); ok && resp.GetCode() == 202 {
				// These samples have been deduped.
				dedupedSamples.WithLabelValues(userID, cluster).Add(float64(numSamples))
			}

			// Ensure the request slice is reused if the series get deduped.
			client.ReuseSlice(req.Timeseries)

			return nil, err
		}
		// If there wasn't an error but removeReplica is false that means we didn't find both HA labels.
		if !removeReplica {
			nonHASamples.WithLabelValues(userID).Add(float64(numSamples))
		}
	}

	latestSampleTimestampMs := int64(0)
	defer func() {
		// Update this metric even in case of errors.
		if latestSampleTimestampMs > 0 {
			latestSeenSampleTimestampPerUser.WithLabelValues(userID).Set(float64(latestSampleTimestampMs) / 1000)
		}
	}()

	// For each timeseries, compute a hash to distribute across ingesters;
	// check each sample and discard if outside limits.
	for _, ts := range req.Timeseries {
		// Use timestamp of latest sample in the series. If samples for series are not ordered, metric for user may be wrong.
		if len(ts.Samples) > 0 {
			latestSampleTimestampMs = util.Max64(latestSampleTimestampMs, ts.Samples[len(ts.Samples)-1].TimestampMs)
		}

		// If we found both the cluster and replica labels, we only want to include the cluster label when
		// storing series in Cortex. If we kept the replica label we would end up with another series for the same
		// series we're trying to dedupe when HA tracking moves over to a different replica.
		if removeReplica {
			removeLabel(d.limits.HAReplicaLabel(userID), &ts.Labels)
		}

		for _, labelName := range d.limits.DropLabels(userID) {
			removeLabel(labelName, &ts.Labels)
		}

		if len(ts.Labels) == 0 {
			continue
		}

		// We rely on sorted labels in different places:
		// 1) When computing token for labels, and sharding by all labels. Here different order of labels returns
		// different tokens, which is bad.
		// 2) In validation code, when checking for duplicate label names. As duplicate label names are rejected
		// later in the validation phase, we ignore them here.
		sortLabelsIfNeeded(ts.Labels)

		// Generate the sharding token based on the series labels without the HA replica
		// label and dropped labels (if any)
		key, err := d.tokenForLabels(userID, ts.Labels)
		if err != nil {
			return nil, err
		}

		validatedSeries, err := d.validateSeries(ts, userID)

		// Errors in validation are considered non-fatal, as one series in a request may contain
		// invalid data but all the remaining series could be perfectly valid.
		if err != nil && firstPartialErr == nil {
			firstPartialErr = err
		}

		// validateSeries would have returned an emptyPreallocSeries if there were no valid samples.
		if validatedSeries == emptyPreallocSeries {
			continue
		}

		seriesKeys = append(seriesKeys, key)
		validatedTimeseries = append(validatedTimeseries, validatedSeries)
		validatedSamples += len(ts.Samples)
	}

	for _, m := range req.Metadata {
		err := validation.ValidateMetadata(d.limits, userID, m)

		if err != nil {
			if firstPartialErr == nil {
				firstPartialErr = err
			}

			continue
		}

		metadataKeys = append(metadataKeys, d.tokenForMetadata(userID, m.MetricName))
		validatedMetadata = append(validatedMetadata, m)
	}

	receivedSamples.WithLabelValues(userID).Add(float64(validatedSamples))
	receivedMetadata.WithLabelValues(userID).Add(float64(len(validatedMetadata)))

	if len(seriesKeys) == 0 && len(metadataKeys) == 0 {
		// Ensure the request slice is reused if there's no series or metadata passing the validation.
		client.ReuseSlice(req.Timeseries)

		return &client.WriteResponse{}, firstPartialErr
	}

	now := time.Now()
	totalN := validatedSamples + len(validatedMetadata)
	if !d.ingestionRateLimiter.AllowN(now, userID, totalN) {
		// Ensure the request slice is reused if the request is rate limited.
		client.ReuseSlice(req.Timeseries)

		// Return a 4xx here to have the client discard the data and not retry. If a client
		// is sending too much data consistently we will unlikely ever catch up otherwise.
		validation.DiscardedSamples.WithLabelValues(validation.RateLimited, userID).Add(float64(validatedSamples))
		validation.DiscardedMetadata.WithLabelValues(validation.RateLimited, userID).Add(float64(len(validatedMetadata)))
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, "ingestion rate limit (%v) exceeded while adding %d samples and %d metadata", d.ingestionRateLimiter.Limit(now, userID), validatedSamples, len(validatedMetadata))
	}

	var subRing ring.ReadRing
	subRing = d.ingestersRing

	// Obtain a subring if required
	if size := d.limits.SubringSize(userID); size > 0 {
		h := client.HashAdd32a(client.HashNew32a(), userID)
		subRing = d.ingestersRing.Subring(h, size)
	}

	keys := append(seriesKeys, metadataKeys...)
	initialMetadataIndex := len(seriesKeys)

	err = ring.DoBatch(ctx, subRing, keys, func(ingester ring.IngesterDesc, indexes []int) error {
		timeseries := make([]client.PreallocTimeseries, 0, len(indexes))
		var metadata []*client.MetricMetadata

		for _, i := range indexes {
			if i >= initialMetadataIndex {
				metadata = append(metadata, validatedMetadata[i-initialMetadataIndex])
			} else {
				timeseries = append(timeseries, validatedTimeseries[i])
			}
		}

		// Use a background context to make sure all ingesters get samples even if we return early
		localCtx, cancel := context.WithTimeout(context.Background(), d.cfg.RemoteTimeout)
		defer cancel()
		localCtx = user.InjectOrgID(localCtx, userID)
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			localCtx = opentracing.ContextWithSpan(localCtx, sp)
		}

		// Get clientIP(s) from Context and add it to localCtx
		localCtx = util.AddSourceIPsToOutgoingContext(localCtx, source)

		return d.send(localCtx, ingester, timeseries, metadata, req.Source)
	}, func() { client.ReuseSlice(req.Timeseries) })
	if err != nil {
		return nil, err
	}
	return &client.WriteResponse{}, firstPartialErr
}

func sortLabelsIfNeeded(labels []client.LabelAdapter) {
	// no need to run sort.Slice, if labels are already sorted, which is most of the time.
	// we can avoid extra memory allocations (mostly interface-related) this way.
	sorted := true
	last := ""
	for _, l := range labels {
		if strings.Compare(last, l.Name) > 0 {
			sorted = false
			break
		}
		last = l.Name
	}

	if sorted {
		return
	}

	sort.Slice(labels, func(i, j int) bool {
		return strings.Compare(labels[i].Name, labels[j].Name) < 0
	})
}

func (d *Distributor) send(ctx context.Context, ingester ring.IngesterDesc, timeseries []client.PreallocTimeseries, metadata []*client.MetricMetadata, source client.WriteRequest_SourceEnum) error {
	h, err := d.ingesterPool.GetClientFor(ingester.Addr)
	if err != nil {
		return err
	}
	c := h.(ingester_client.IngesterClient)

	req := client.WriteRequest{
		Timeseries: timeseries,
		Metadata:   metadata,
		Source:     source,
	}
	_, err = c.Push(ctx, &req)

	if len(metadata) > 0 {
		ingesterAppends.WithLabelValues(ingester.Addr, typeMetadata).Inc()
		if err != nil {
			ingesterAppendFailures.WithLabelValues(ingester.Addr, typeMetadata).Inc()
		}
	}
	if len(timeseries) > 0 {
		ingesterAppends.WithLabelValues(ingester.Addr, typeSamples).Inc()
		if err != nil {
			ingesterAppendFailures.WithLabelValues(ingester.Addr, typeSamples).Inc()
		}
	}

	return err
}

// forAllIngesters runs f, in parallel, for all ingesters
func (d *Distributor) forAllIngesters(ctx context.Context, reallyAll bool, f func(client.IngesterClient) (interface{}, error)) ([]interface{}, error) {
	replicationSet, err := d.ingestersRing.GetAll(ring.Read)
	if err != nil {
		return nil, err
	}
	if reallyAll {
		replicationSet.MaxErrors = 0
	}

	return replicationSet.Do(ctx, d.cfg.ExtraQueryDelay, func(ing *ring.IngesterDesc) (interface{}, error) {
		client, err := d.ingesterPool.GetClientFor(ing.Addr)
		if err != nil {
			return nil, err
		}

		return f(client.(ingester_client.IngesterClient))
	})
}

// LabelValuesForLabelName returns all of the label values that are associated with a given label name.
func (d *Distributor) LabelValuesForLabelName(ctx context.Context, labelName model.LabelName) ([]string, error) {
	req := &client.LabelValuesRequest{
		LabelName: string(labelName),
	}
	resps, err := d.forAllIngesters(ctx, false, func(client client.IngesterClient) (interface{}, error) {
		return client.LabelValues(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	valueSet := map[string]struct{}{}
	for _, resp := range resps {
		for _, v := range resp.(*client.LabelValuesResponse).LabelValues {
			valueSet[v] = struct{}{}
		}
	}

	values := make([]string, 0, len(valueSet))
	for v := range valueSet {
		values = append(values, v)
	}
	return values, nil
}

// LabelNames returns all of the label names.
func (d *Distributor) LabelNames(ctx context.Context) ([]string, error) {
	req := &client.LabelNamesRequest{}
	resps, err := d.forAllIngesters(ctx, false, func(client client.IngesterClient) (interface{}, error) {
		return client.LabelNames(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	valueSet := map[string]struct{}{}
	for _, resp := range resps {
		for _, v := range resp.(*client.LabelNamesResponse).LabelNames {
			valueSet[v] = struct{}{}
		}
	}

	values := make([]string, 0, len(valueSet))
	for v := range valueSet {
		values = append(values, v)
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i] < values[j]
	})

	return values, nil
}

// MetricsForLabelMatchers gets the metrics that match said matchers
func (d *Distributor) MetricsForLabelMatchers(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]metric.Metric, error) {
	req, err := ingester_client.ToMetricsForLabelMatchersRequest(from, through, matchers)
	if err != nil {
		return nil, err
	}

	resps, err := d.forAllIngesters(ctx, false, func(client client.IngesterClient) (interface{}, error) {
		return client.MetricsForLabelMatchers(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	metrics := map[model.Fingerprint]model.Metric{}
	for _, resp := range resps {
		ms := ingester_client.FromMetricsForLabelMatchersResponse(resp.(*client.MetricsForLabelMatchersResponse))
		for _, m := range ms {
			metrics[m.Fingerprint()] = m
		}
	}

	result := make([]metric.Metric, 0, len(metrics))
	for _, m := range metrics {
		result = append(result, metric.Metric{
			Metric: m,
		})
	}
	return result, nil
}

// MetricsMetadata returns all metric metadata of a user.
func (d *Distributor) MetricsMetadata(ctx context.Context) ([]scrape.MetricMetadata, error) {
	req := &ingester_client.MetricsMetadataRequest{}
	// TODO(gotjosh): We only need to look in all the ingesters if shardByAllLabels is enabled.
	resps, err := d.forAllIngesters(ctx, false, func(client client.IngesterClient) (interface{}, error) {
		return client.MetricsMetadata(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	result := []scrape.MetricMetadata{}
	dedupTracker := map[ingester_client.MetricMetadata]struct{}{}
	for _, resp := range resps {
		r := resp.(*client.MetricsMetadataResponse)
		for _, m := range r.Metadata {
			// Given we look across all ingesters - dedup the metadata.
			_, ok := dedupTracker[*m]
			if ok {
				continue
			}
			dedupTracker[*m] = struct{}{}

			result = append(result, scrape.MetricMetadata{
				Metric: m.MetricName,
				Help:   m.Help,
				Unit:   m.Unit,
				Type:   client.MetricMetadataMetricTypeToMetricType(m.GetType()),
			})
		}
	}

	return result, nil
}

// UserStats returns statistics about the current user.
func (d *Distributor) UserStats(ctx context.Context) (*UserStats, error) {
	req := &client.UserStatsRequest{}
	resps, err := d.forAllIngesters(ctx, true, func(client client.IngesterClient) (interface{}, error) {
		return client.UserStats(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	totalStats := &UserStats{}
	for _, resp := range resps {
		r := resp.(*client.UserStatsResponse)
		totalStats.IngestionRate += r.IngestionRate
		totalStats.APIIngestionRate += r.ApiIngestionRate
		totalStats.RuleIngestionRate += r.RuleIngestionRate
		totalStats.NumSeries += r.NumSeries
	}

	totalStats.IngestionRate /= float64(d.ingestersRing.ReplicationFactor())
	totalStats.NumSeries /= uint64(d.ingestersRing.ReplicationFactor())

	return totalStats, nil
}

// UserIDStats models ingestion statistics for one user, including the user ID
type UserIDStats struct {
	UserID string `json:"userID"`
	UserStats
}

// AllUserStats returns statistics about all users.
// Note it does not divide by the ReplicationFactor like UserStats()
func (d *Distributor) AllUserStats(ctx context.Context) ([]UserIDStats, error) {
	// Add up by user, across all responses from ingesters
	perUserTotals := make(map[string]UserStats)

	req := &client.UserStatsRequest{}
	ctx = user.InjectOrgID(ctx, "1") // fake: ingester insists on having an org ID
	// Not using d.forAllIngesters(), so we can fail after first error.
	replicationSet, err := d.ingestersRing.GetAll(ring.Read)
	if err != nil {
		return nil, err
	}
	for _, ingester := range replicationSet.Ingesters {
		client, err := d.ingesterPool.GetClientFor(ingester.Addr)
		if err != nil {
			return nil, err
		}
		resp, err := client.(ingester_client.IngesterClient).AllUserStats(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, u := range resp.Stats {
			s := perUserTotals[u.UserId]
			s.IngestionRate += u.Data.IngestionRate
			s.APIIngestionRate += u.Data.ApiIngestionRate
			s.RuleIngestionRate += u.Data.RuleIngestionRate
			s.NumSeries += u.Data.NumSeries
			perUserTotals[u.UserId] = s
		}
	}

	// Turn aggregated map into a slice for return
	response := make([]UserIDStats, 0, len(perUserTotals))
	for id, stats := range perUserTotals {
		response = append(response, UserIDStats{
			UserID: id,
			UserStats: UserStats{
				IngestionRate:     stats.IngestionRate,
				APIIngestionRate:  stats.APIIngestionRate,
				RuleIngestionRate: stats.RuleIngestionRate,
				NumSeries:         stats.NumSeries,
			},
		})
	}

	return response, nil
}
