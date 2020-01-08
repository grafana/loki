package distributor

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/health/grpc_health_v1"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	ingester_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/prom1/storage/metric"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/extract"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/limiter"
	"github.com/cortexproject/cortex/pkg/util/validation"
	billing "github.com/weaveworks/billing-client"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/user"
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
	incomingSamples = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_samples_in_total",
		Help:      "The total number of samples that have come in to the distributor, including rejected or deduped samples.",
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
	}, []string{"ingester"})
	ingesterAppendFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "distributor_ingester_append_failures_total",
		Help:      "The total number of failed batch appends sent to ingesters.",
	}, []string{"ingester"})
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
	emptyPreallocSeries = ingester_client.PreallocTimeseries{}
)

// Distributor is a storage.SampleAppender and a client.Querier which
// forwards appends and queries to individual ingesters.
type Distributor struct {
	cfg           Config
	ingestersRing ring.ReadRing
	ingesterPool  *ingester_client.Pool
	limits        *validation.Overrides
	billingClient *billing.Client

	// The global rate limiter requires a distributors ring to count
	// the number of healthy instances
	distributorsRing *ring.Lifecycler

	// For handling HA replicas.
	Replicas *haTracker

	// Per-user rate limiter.
	ingestionRateLimiter *limiter.RateLimiter
}

// Config contains the configuration require to
// create a Distributor
type Config struct {
	EnableBilling bool                       `yaml:"enable_billing,omitempty"`
	BillingConfig billing.Config             `yaml:"billing,omitempty"`
	PoolConfig    ingester_client.PoolConfig `yaml:"pool,omitempty"`

	HATrackerConfig HATrackerConfig `yaml:"ha_tracker,omitempty"`

	MaxRecvMsgSize  int           `yaml:"max_recv_msg_size"`
	RemoteTimeout   time.Duration `yaml:"remote_timeout,omitempty"`
	ExtraQueryDelay time.Duration `yaml:"extra_queue_delay,omitempty"`

	ShardByAllLabels bool `yaml:"shard_by_all_labels,omitempty"`

	// Distributors ring
	DistributorRing RingConfig `yaml:"ring,omitempty"`

	// for testing
	ingesterClientFactory client.Factory `yaml:"-"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.BillingConfig.RegisterFlags(f)
	cfg.PoolConfig.RegisterFlags(f)
	cfg.HATrackerConfig.RegisterFlags(f)
	cfg.DistributorRing.RegisterFlags(f)

	f.BoolVar(&cfg.EnableBilling, "distributor.enable-billing", false, "Report number of ingested samples to billing system.")
	f.IntVar(&cfg.MaxRecvMsgSize, "distributor.max-recv-msg-size", 100<<20, "remote_write API max receive message size (bytes).")
	f.DurationVar(&cfg.RemoteTimeout, "distributor.remote-timeout", 2*time.Second, "Timeout for downstream ingesters.")
	f.DurationVar(&cfg.ExtraQueryDelay, "distributor.extra-query-delay", 0, "Time to wait before sending more than the minimum successful query requests.")
	flagext.DeprecatedFlag(f, "distributor.limiter-reload-period", "DEPRECATED. No more required because the local limiter is reconfigured as soon as the overrides change.")
	f.BoolVar(&cfg.ShardByAllLabels, "distributor.shard-by-all-labels", false, "Distribute samples based on all labels, as opposed to solely by user and metric name.")
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	return cfg.HATrackerConfig.Validate()
}

// New constructs a new Distributor
func New(cfg Config, clientConfig ingester_client.Config, limits *validation.Overrides, ingestersRing ring.ReadRing, canJoinDistributorsRing bool) (*Distributor, error) {
	if cfg.ingesterClientFactory == nil {
		cfg.ingesterClientFactory = func(addr string) (grpc_health_v1.HealthClient, error) {
			return ingester_client.MakeIngesterClient(addr, clientConfig)
		}
	}

	var billingClient *billing.Client
	if cfg.EnableBilling {
		var err error
		billingClient, err = billing.NewClient(cfg.BillingConfig)
		if err != nil {
			return nil, err
		}
	}

	replicationFactor.Set(float64(ingestersRing.ReplicationFactor()))
	cfg.PoolConfig.RemoteTimeout = cfg.RemoteTimeout

	replicas, err := newClusterTracker(cfg.HATrackerConfig)
	if err != nil {
		return nil, err
	}

	// Create the configured ingestion rate limit strategy (local or global). In case
	// it's an internal dependency and can't join the distributors ring, we skip rate
	// limiting.
	var ingestionRateStrategy limiter.RateLimiterStrategy
	var distributorsRing *ring.Lifecycler

	if !canJoinDistributorsRing {
		ingestionRateStrategy = newInfiniteIngestionRateStrategy()
	} else if limits.IngestionRateStrategy() == validation.GlobalIngestionRateStrategy {
		distributorsRing, err = ring.NewLifecycler(cfg.DistributorRing.ToLifecyclerConfig(), nil, "distributor", ring.DistributorRingKey)
		if err != nil {
			return nil, err
		}

		distributorsRing.Start()

		ingestionRateStrategy = newGlobalIngestionRateStrategy(limits, distributorsRing)
	} else {
		ingestionRateStrategy = newLocalIngestionRateStrategy(limits)
	}

	d := &Distributor{
		cfg:                  cfg,
		ingestersRing:        ingestersRing,
		ingesterPool:         ingester_client.NewPool(cfg.PoolConfig, ingestersRing, cfg.ingesterClientFactory, util.Logger),
		billingClient:        billingClient,
		distributorsRing:     distributorsRing,
		limits:               limits,
		ingestionRateLimiter: limiter.NewRateLimiter(ingestionRateStrategy, 10*time.Second),
		Replicas:             replicas,
	}

	return d, nil
}

// Stop stops the distributor's maintenance loop.
func (d *Distributor) Stop() {
	d.ingesterPool.Stop()
	d.Replicas.stop()

	if d.distributorsRing != nil {
		d.distributorsRing.Shutdown()
	}
}

func (d *Distributor) tokenForLabels(userID string, labels []client.LabelAdapter) (uint32, error) {
	if d.cfg.ShardByAllLabels {
		return shardByAllLabels(userID, labels)
	}

	metricName, err := extract.MetricNameFromLabelAdapters(labels)
	if err != nil {
		return 0, err
	}
	return shardByMetricName(userID, metricName), nil
}

func shardByMetricName(userID string, metricName string) uint32 {
	h := client.HashNew32()
	h = client.HashAdd32(h, userID)
	h = client.HashAdd32(h, metricName)
	return h
}

func shardByAllLabels(userID string, labels []client.LabelAdapter) (uint32, error) {
	h := client.HashNew32()
	h = client.HashAdd32(h, userID)
	var lastLabelName string
	for _, label := range labels {
		if strings.Compare(lastLabelName, label.Name) >= 0 {
			return 0, fmt.Errorf("Labels not sorted")
		}
		h = client.HashAdd32(h, label.Name)
		h = client.HashAdd32(h, label.Value)
	}
	return h, nil
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
	err := d.Replicas.checkReplica(ctx, userID, cluster, replica)
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
	if err := validation.ValidateLabels(d.limits, userID, ts.Labels); err != nil {
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

	var lastPartialErr error
	removeReplica := false

	numSamples := 0
	for _, ts := range req.Timeseries {
		numSamples += len(ts.Samples)
	}
	// Count the total samples in, prior to validation or deuplication, for comparison with other metrics.
	incomingSamples.WithLabelValues(userID).Add(float64(numSamples))

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

	// For each timeseries, compute a hash to distribute across ingesters;
	// check each sample and discard if outside limits.
	validatedTimeseries := make([]client.PreallocTimeseries, 0, len(req.Timeseries))
	keys := make([]uint32, 0, len(req.Timeseries))
	validatedSamples := 0
	for _, ts := range req.Timeseries {
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

		// Generate the sharding token based on the series labels without the HA replica
		// label and dropped labels (if any)
		key, err := d.tokenForLabels(userID, ts.Labels)
		if err != nil {
			return nil, err
		}

		validatedSeries, err := d.validateSeries(ts, userID)

		// Errors in validation are considered non-fatal, as one series in a request may contain
		// invalid data but all the remaining series could be perfectly valid.
		if err != nil {
			lastPartialErr = err
		}

		// validateSeries would have returned an emptyPreallocSeries if there were no valid samples.
		if validatedSeries == emptyPreallocSeries {
			continue
		}

		metricName, _ := extract.MetricNameFromLabelAdapters(ts.Labels)
		samples := make([]client.Sample, 0, len(ts.Samples))
		for _, s := range ts.Samples {
			if err := validation.ValidateSample(d.limits, userID, metricName, s); err != nil {
				lastPartialErr = err
				continue
			}
			samples = append(samples, s)
		}

		keys = append(keys, key)
		validatedTimeseries = append(validatedTimeseries, validatedSeries)
		validatedSamples += len(ts.Samples)
	}
	receivedSamples.WithLabelValues(userID).Add(float64(validatedSamples))

	if len(keys) == 0 {
		// Ensure the request slice is reused if there's no series passing the validation.
		client.ReuseSlice(req.Timeseries)

		return &client.WriteResponse{}, lastPartialErr
	}

	now := time.Now()
	if !d.ingestionRateLimiter.AllowN(now, userID, validatedSamples) {
		// Ensure the request slice is reused if the request is rate limited.
		client.ReuseSlice(req.Timeseries)

		// Return a 4xx here to have the client discard the data and not retry. If a client
		// is sending too much data consistently we will unlikely ever catch up otherwise.
		validation.DiscardedSamples.WithLabelValues(validation.RateLimited, userID).Add(float64(validatedSamples))
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, "ingestion rate limit (%v) exceeded while adding %d samples", d.ingestionRateLimiter.Limit(now, userID), numSamples)
	}

	err = ring.DoBatch(ctx, d.ingestersRing, keys, func(ingester ring.IngesterDesc, indexes []int) error {
		timeseries := make([]client.PreallocTimeseries, 0, len(indexes))
		for _, i := range indexes {
			timeseries = append(timeseries, validatedTimeseries[i])
		}

		// Use a background context to make sure all ingesters get samples even if we return early
		localCtx, cancel := context.WithTimeout(context.Background(), d.cfg.RemoteTimeout)
		defer cancel()
		localCtx = user.InjectOrgID(localCtx, userID)
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			localCtx = opentracing.ContextWithSpan(localCtx, sp)
		}
		return d.sendSamples(localCtx, ingester, timeseries)
	}, func() { client.ReuseSlice(req.Timeseries) })
	if err != nil {
		return nil, err
	}
	return &client.WriteResponse{}, lastPartialErr
}

func (d *Distributor) sendSamples(ctx context.Context, ingester ring.IngesterDesc, timeseries []client.PreallocTimeseries) error {
	h, err := d.ingesterPool.GetClientFor(ingester.Addr)
	if err != nil {
		return err
	}
	c := h.(ingester_client.IngesterClient)

	req := client.WriteRequest{
		Timeseries: timeseries,
	}
	_, err = c.Push(ctx, &req)

	ingesterAppends.WithLabelValues(ingester.Addr).Inc()
	if err != nil {
		ingesterAppendFailures.WithLabelValues(ingester.Addr).Inc()
	}
	return err
}

// forAllIngesters runs f, in parallel, for all ingesters
func (d *Distributor) forAllIngesters(ctx context.Context, reallyAll bool, f func(client.IngesterClient) (interface{}, error)) ([]interface{}, error) {
	replicationSet, err := d.ingestersRing.GetAll()
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
	replicationSet, err := d.ingestersRing.GetAll()
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
