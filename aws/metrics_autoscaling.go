package aws

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	promApi "github.com/prometheus/client_golang/api"
	promV1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"

	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/util"
)

const (
	cachePromDataFor       = 30 * time.Second
	queueObservationPeriod = 2 * time.Minute
	targetScaledown        = 0.1 // consider scaling down if queue smaller than this times target
	targetMax              = 10  // always scale up if queue bigger than this times target
	errorFractionScaledown = 0.1
	minUsageForScaledown   = 100 // only scale down if usage is > this DynamoDB units/sec

	// fetch Ingester queue length
	// average the queue length over 2 minutes to avoid aliasing with the 1-minute flush period
	defaultQueueLenQuery = `sum(avg_over_time(cortex_ingester_flush_queue_length{job="cortex/ingester"}[2m]))`
	// fetch write error rate per DynamoDB table
	defaultErrorRateQuery = `sum(rate(cortex_dynamo_failures_total{error="ProvisionedThroughputExceededException",operation=~".*Write.*"}[1m])) by (table) > 0`
	// fetch write capacity usage per DynamoDB table
	// use the rate over 15 minutes so we take a broad average
	defaultUsageQuery = `sum(rate(cortex_dynamo_consumed_capacity_total{operation="DynamoDB.BatchWriteItem"}[15m])) by (table) > 0`
)

// MetricsAutoScalingConfig holds parameters to configure how it works
type MetricsAutoScalingConfig struct {
	URL              string  // URL to contact Prometheus store on
	TargetQueueLen   int64   // Queue length above which we will scale up capacity
	ScaleUpFactor    float64 // Scale up capacity by this multiple
	QueueLengthQuery string  // Promql query to fetch ingester queue length
	ErrorRateQuery   string  // Promql query to fetch error rates per table
	UsageQuery       string  // Promql query to fetch write capacity usage per table
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *MetricsAutoScalingConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.URL, "metrics.url", "", "Use metrics-based autoscaling, via this query URL")
	f.Int64Var(&cfg.TargetQueueLen, "metrics.target-queue-length", 100000, "Queue length above which we will scale up capacity")
	f.Float64Var(&cfg.ScaleUpFactor, "metrics.scale-up-factor", 1.3, "Scale up capacity by this multiple")
	f.StringVar(&cfg.QueueLengthQuery, "metrics.queue-length-query", defaultQueueLenQuery, "query to fetch ingester queue length")
	f.StringVar(&cfg.ErrorRateQuery, "metrics.error-rate-query", defaultErrorRateQuery, "query to fetch error rates per table")
	f.StringVar(&cfg.UsageQuery, "metrics.usage-query", defaultUsageQuery, "query to fetch write capacity usage per table")
}

type metricsData struct {
	cfg              MetricsAutoScalingConfig
	promAPI          promV1.API
	promLastQuery    time.Time
	tableLastUpdated map[string]time.Time
	queueLengths     []float64
	errorRates       map[string]float64
	usageRates       map[string]float64
}

func newMetrics(cfg DynamoDBConfig) (*metricsData, error) {
	client, err := promApi.NewClient(promApi.Config{Address: cfg.Metrics.URL})
	if err != nil {
		return nil, err
	}
	return &metricsData{
		promAPI:          promV1.NewAPI(client),
		cfg:              cfg.Metrics,
		tableLastUpdated: make(map[string]time.Time),
	}, nil
}

func (m *metricsData) PostCreateTable(ctx context.Context, desc chunk.TableDesc) error {
	return nil
}

func (m *metricsData) DescribeTable(ctx context.Context, desc *chunk.TableDesc) error {
	return nil
}

func (m *metricsData) UpdateTable(ctx context.Context, current chunk.TableDesc, expected *chunk.TableDesc) error {
	if err := m.update(ctx); err != nil {
		return err
	}

	errorRate := m.errorRates[expected.Name]
	usageRate := m.usageRates[expected.Name]

	level.Info(util.Logger).Log("msg", "checking metrics", "table", current.Name, "queueLengths", fmt.Sprint(m.queueLengths), "errorRate", errorRate, "usageRate", usageRate)

	// If we don't take explicit action, return the current provision as the expected provision
	expected.ProvisionedWrite = current.ProvisionedWrite

	switch {
	case errorRate < errorFractionScaledown*float64(current.ProvisionedWrite) &&
		m.queueLengths[2] < float64(m.cfg.TargetQueueLen)*targetScaledown:
		// No big queue, low errors -> scale down
		m.scaleDownWrite(current, expected, m.computeScaleDown(current, *expected), "metrics scale-down")
	case errorRate == 0 &&
		m.queueLengths[2] < m.queueLengths[1] && m.queueLengths[1] < m.queueLengths[0]:
		// zero errors and falling queue -> scale down to current usage
		m.scaleDownWrite(current, expected, m.computeScaleDown(current, *expected), "zero errors scale-down")
	case errorRate > 0 && m.queueLengths[2] > float64(m.cfg.TargetQueueLen)*targetMax:
		// Too big queue, some errors -> scale up
		m.scaleUpWrite(current, expected, m.computeScaleUp(current, *expected), "metrics max queue scale-up")
	case errorRate > 0 &&
		m.queueLengths[2] > float64(m.cfg.TargetQueueLen) &&
		m.queueLengths[2] > m.queueLengths[1] && m.queueLengths[1] > m.queueLengths[0]:
		// Growing queue, some errors -> scale up
		m.scaleUpWrite(current, expected, m.computeScaleUp(current, *expected), "metrics queue growing scale-up")
	}
	return nil
}

func (m metricsData) computeScaleDown(current, expected chunk.TableDesc) int64 {
	usageRate := m.usageRates[expected.Name]
	return int64(usageRate * 100.0 / expected.WriteScale.TargetValue)
}

func (m metricsData) computeScaleUp(current, expected chunk.TableDesc) int64 {
	scaleUp := int64(float64(current.ProvisionedWrite) * m.cfg.ScaleUpFactor)
	// Scale up minimum of 10% of max capacity, to avoid futzing around at low levels
	minIncrement := expected.WriteScale.MaxCapacity / 10
	if scaleUp < current.ProvisionedWrite+minIncrement {
		scaleUp = current.ProvisionedWrite + minIncrement
	}
	return scaleUp
}

func (m *metricsData) scaleDownWrite(current chunk.TableDesc, expected *chunk.TableDesc, newWrite int64, msg string) {
	if newWrite < expected.WriteScale.MinCapacity {
		newWrite = expected.WriteScale.MinCapacity
	}
	// If we're already at or below the requested value, it's not a scale-down.
	if newWrite >= current.ProvisionedWrite {
		return
	}
	earliest := m.tableLastUpdated[current.Name].Add(time.Duration(expected.WriteScale.InCooldown) * time.Second)
	if earliest.After(mtime.Now()) {
		level.Info(util.Logger).Log("msg", "deferring "+msg, "table", current.Name, "till", earliest)
		return
	}
	// Reject a change that is less than 20% - AWS rate-limits scale-downs so save
	// our chances until it makes a bigger difference
	if newWrite > current.ProvisionedWrite*4/5 {
		level.Info(util.Logger).Log("msg", "rejected de minimis "+msg, "table", current.Name, "current", current.ProvisionedWrite, "proposed", newWrite)
		return
	}
	// Check that the ingesters seem to be doing some work - don't want to scale down
	// if all our metrics are returning zero, or all the ingesters have crashed, etc
	totalUsage := 0.0
	for _, u := range m.usageRates {
		totalUsage += u
	}
	if totalUsage < minUsageForScaledown {
		level.Info(util.Logger).Log("msg", "rejected low usage "+msg, "table", current.Name, "totalUsage", totalUsage)
		return
	}

	level.Info(util.Logger).Log("msg", msg, "table", current.Name, "write", newWrite)
	expected.ProvisionedWrite = newWrite
	m.tableLastUpdated[current.Name] = mtime.Now()
}

func (m *metricsData) scaleUpWrite(current chunk.TableDesc, expected *chunk.TableDesc, newWrite int64, msg string) {
	if newWrite > expected.WriteScale.MaxCapacity {
		newWrite = expected.WriteScale.MaxCapacity
	}
	earliest := m.tableLastUpdated[current.Name].Add(time.Duration(expected.WriteScale.OutCooldown) * time.Second)
	if earliest.After(mtime.Now()) {
		level.Info(util.Logger).Log("msg", "deferring "+msg, "table", current.Name, "till", earliest)
		return
	}
	if newWrite > current.ProvisionedWrite {
		level.Info(util.Logger).Log("msg", msg, "table", current.Name, "write", newWrite)
		expected.ProvisionedWrite = newWrite
		m.tableLastUpdated[current.Name] = mtime.Now()
	}
}

func (m *metricsData) update(ctx context.Context) error {
	if m.promLastQuery.After(mtime.Now().Add(-cachePromDataFor)) {
		return nil
	}

	m.promLastQuery = mtime.Now()
	qlMatrix, err := promQuery(ctx, m.promAPI, m.cfg.QueueLengthQuery, queueObservationPeriod, queueObservationPeriod/2)
	if err != nil {
		return err
	}
	if len(qlMatrix) != 1 {
		return errors.Errorf("expected one sample stream for queue: %d", len(qlMatrix))
	}
	if len(qlMatrix[0].Values) != 3 {
		return errors.Errorf("expected three values: %d", len(qlMatrix[0].Values))
	}
	m.queueLengths = make([]float64, len(qlMatrix[0].Values))
	for i, v := range qlMatrix[0].Values {
		m.queueLengths[i] = float64(v.Value)
	}

	deMatrix, err := promQuery(ctx, m.promAPI, m.cfg.ErrorRateQuery, 0, time.Second)
	if err != nil {
		return err
	}
	if m.errorRates, err = extractRates(deMatrix); err != nil {
		return err
	}

	usageMatrix, err := promQuery(ctx, m.promAPI, m.cfg.UsageQuery, 0, time.Second)
	if err != nil {
		return err
	}
	if m.usageRates, err = extractRates(usageMatrix); err != nil {
		return err
	}

	return nil
}

func extractRates(matrix model.Matrix) (map[string]float64, error) {
	ret := map[string]float64{}
	for _, s := range matrix {
		table, found := s.Metric["table"]
		if !found {
			continue
		}
		if len(s.Values) != 1 {
			return nil, errors.Errorf("expected one sample for table %s: %d", table, len(s.Values))
		}
		ret[string(table)] = float64(s.Values[0].Value)
	}
	return ret, nil
}

func promQuery(ctx context.Context, promAPI promV1.API, query string, duration, step time.Duration) (model.Matrix, error) {
	queryRange := promV1.Range{
		Start: mtime.Now().Add(-duration),
		End:   mtime.Now(),
		Step:  step,
	}

	value, err := promAPI.QueryRange(ctx, query, queryRange)
	if err != nil {
		return nil, err
	}
	matrix, ok := value.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("Unable to convert value to matrix: %#v", value)
	}
	return matrix, nil
}
