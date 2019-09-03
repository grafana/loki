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

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
)

const (
	cachePromDataFor          = 30 * time.Second
	queueObservationPeriod    = 2 * time.Minute
	targetScaledown           = 0.1 // consider scaling down if queue smaller than this times target
	targetMax                 = 10  // always scale up if queue bigger than this times target
	throttleFractionScaledown = 0.1
	minUsageForScaledown      = 100 // only scale down if usage is > this DynamoDB units/sec

	// fetch Ingester queue length
	// average the queue length over 2 minutes to avoid aliasing with the 1-minute flush period
	defaultQueueLenQuery = `sum(avg_over_time(cortex_ingester_flush_queue_length{job="cortex/ingester"}[2m]))`
	// fetch write throttle rate per DynamoDB table
	defaultThrottleRateQuery = `sum(rate(cortex_dynamo_throttled_total{operation="DynamoDB.BatchWriteItem"}[1m])) by (table) > 0`
	// fetch write capacity usage per DynamoDB table
	// use the rate over 15 minutes so we take a broad average
	defaultUsageQuery = `sum(rate(cortex_dynamo_consumed_capacity_total{operation="DynamoDB.BatchWriteItem"}[15m])) by (table) > 0`
	// use the read rate over 1hr so we take a broad average
	defaultReadUsageQuery = `sum(rate(cortex_dynamo_consumed_capacity_total{operation="DynamoDB.QueryPages"}[1h])) by (table) > 0`
	// fetch read error rate per DynamoDB table
	defaultReadErrorQuery = `sum(increase(cortex_dynamo_failures_total{operation="DynamoDB.QueryPages",error="ProvisionedThroughputExceededException"}[1m])) by (table) > 0`
)

// MetricsAutoScalingConfig holds parameters to configure how it works
type MetricsAutoScalingConfig struct {
	URL              string  // URL to contact Prometheus store on
	TargetQueueLen   int64   // Queue length above which we will scale up capacity
	ScaleUpFactor    float64 // Scale up capacity by this multiple
	MinThrottling    float64 // Ignore throttling below this level
	QueueLengthQuery string  // Promql query to fetch ingester queue length
	ThrottleQuery    string  // Promql query to fetch throttle rate per table
	UsageQuery       string  // Promql query to fetch write capacity usage per table
	ReadUsageQuery   string  // Promql query to fetch read usage per table
	ReadErrorQuery   string  // Promql query to fetch read errors per table

	deprecatedErrorRateQuery string
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *MetricsAutoScalingConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.URL, "metrics.url", "", "Use metrics-based autoscaling, via this query URL")
	f.Int64Var(&cfg.TargetQueueLen, "metrics.target-queue-length", 100000, "Queue length above which we will scale up capacity")
	f.Float64Var(&cfg.ScaleUpFactor, "metrics.scale-up-factor", 1.3, "Scale up capacity by this multiple")
	f.Float64Var(&cfg.MinThrottling, "metrics.ignore-throttle-below", 1, "Ignore throttling below this level (rate per second)")
	f.StringVar(&cfg.QueueLengthQuery, "metrics.queue-length-query", defaultQueueLenQuery, "query to fetch ingester queue length")
	f.StringVar(&cfg.ThrottleQuery, "metrics.write-throttle-query", defaultThrottleRateQuery, "query to fetch throttle rates per table")
	f.StringVar(&cfg.UsageQuery, "metrics.usage-query", defaultUsageQuery, "query to fetch write capacity usage per table")
	f.StringVar(&cfg.ReadUsageQuery, "metrics.read-usage-query", defaultReadUsageQuery, "query to fetch read capacity usage per table")
	f.StringVar(&cfg.ReadErrorQuery, "metrics.read-error-query", defaultReadErrorQuery, "query to fetch read errors per table")

	f.StringVar(&cfg.deprecatedErrorRateQuery, "metrics.error-rate-query", "", "DEPRECATED: use -metrics.write-throttle-query instead")
}

type metricsData struct {
	cfg                  MetricsAutoScalingConfig
	promAPI              promV1.API
	promLastQuery        time.Time
	tableLastUpdated     map[string]time.Time
	tableReadLastUpdated map[string]time.Time
	queueLengths         []float64
	throttleRates        map[string]float64
	usageRates           map[string]float64
	usageReadRates       map[string]float64
	readErrorRates       map[string]float64
}

func newMetrics(cfg DynamoDBConfig) (*metricsData, error) {
	if cfg.Metrics.deprecatedErrorRateQuery != "" {
		level.Warn(util.Logger).Log("msg", "use of deprecated flag -metrics.error-rate-query")
		cfg.Metrics.ThrottleQuery = cfg.Metrics.deprecatedErrorRateQuery
	}
	client, err := promApi.NewClient(promApi.Config{Address: cfg.Metrics.URL})
	if err != nil {
		return nil, err
	}
	return &metricsData{
		promAPI:              promV1.NewAPI(client),
		cfg:                  cfg.Metrics,
		tableLastUpdated:     make(map[string]time.Time),
		tableReadLastUpdated: make(map[string]time.Time),
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

	if expected.WriteScale.Enabled {
		// default if no action is taken is to use the currently provisioned setting
		expected.ProvisionedWrite = current.ProvisionedWrite

		throttleRate := m.throttleRates[expected.Name]
		usageRate := m.usageRates[expected.Name]

		level.Info(util.Logger).Log("msg", "checking write metrics", "table", current.Name, "queueLengths", fmt.Sprint(m.queueLengths), "throttleRate", throttleRate, "usageRate", usageRate)

		switch {
		case throttleRate < throttleFractionScaledown*float64(current.ProvisionedWrite) &&
			m.queueLengths[2] < float64(m.cfg.TargetQueueLen)*targetScaledown:
			// No big queue, low throttling -> scale down
			expected.ProvisionedWrite = scaleDown(current.Name,
				current.ProvisionedWrite,
				expected.WriteScale.MinCapacity,
				computeScaleDown(current.Name, m.usageRates, expected.WriteScale.TargetValue),
				m.tableLastUpdated,
				expected.WriteScale.InCooldown,
				"metrics scale-down",
				"write",
				m.usageRates)
		case throttleRate == 0 &&
			m.queueLengths[2] < m.queueLengths[1] && m.queueLengths[1] < m.queueLengths[0]:
			// zero errors and falling queue -> scale down to current usage
			expected.ProvisionedWrite = scaleDown(current.Name,
				current.ProvisionedWrite,
				expected.WriteScale.MinCapacity,
				computeScaleDown(current.Name, m.usageRates, expected.WriteScale.TargetValue),
				m.tableLastUpdated,
				expected.WriteScale.InCooldown,
				"zero errors scale-down",
				"write",
				m.usageRates)
		case throttleRate > 0 && m.queueLengths[2] > float64(m.cfg.TargetQueueLen)*targetMax:
			// Too big queue, some throttling -> scale up (note we don't apply MinThrottling in this case)
			expected.ProvisionedWrite = scaleUp(current.Name,
				current.ProvisionedWrite,
				expected.WriteScale.MaxCapacity,
				computeScaleUp(current.ProvisionedWrite, expected.WriteScale.MaxCapacity, m.cfg.ScaleUpFactor),
				m.tableLastUpdated,
				expected.WriteScale.OutCooldown,
				"metrics max queue scale-up",
				"write")
		case throttleRate > m.cfg.MinThrottling &&
			m.queueLengths[2] > float64(m.cfg.TargetQueueLen) &&
			m.queueLengths[2] > m.queueLengths[1] && m.queueLengths[1] > m.queueLengths[0]:
			// Growing queue, some throttling -> scale up
			expected.ProvisionedWrite = scaleUp(current.Name,
				current.ProvisionedWrite,
				expected.WriteScale.MaxCapacity,
				computeScaleUp(current.ProvisionedWrite, expected.WriteScale.MaxCapacity, m.cfg.ScaleUpFactor),
				m.tableLastUpdated,
				expected.WriteScale.OutCooldown,
				"metrics queue growing scale-up",
				"write")
		}
	}

	if expected.ReadScale.Enabled {
		// default if no action is taken is to use the currently provisioned setting
		expected.ProvisionedRead = current.ProvisionedRead
		readUsageRate := m.usageReadRates[expected.Name]
		readErrorRate := m.readErrorRates[expected.Name]

		level.Info(util.Logger).Log("msg", "checking read metrics", "table", current.Name, "errorRate", readErrorRate, "readUsageRate", readUsageRate)
		// Read Scaling
		switch {
		// the table is at low/minimum capacity and it is being used -> scale up
		case readUsageRate > 0 && current.ProvisionedRead < expected.ReadScale.MaxCapacity/10:
			expected.ProvisionedRead = scaleUp(
				current.Name,
				current.ProvisionedRead,
				expected.ReadScale.MaxCapacity,
				computeScaleUp(current.ProvisionedRead, expected.ReadScale.MaxCapacity, m.cfg.ScaleUpFactor),
				m.tableReadLastUpdated, expected.ReadScale.OutCooldown,
				"table is being used. scale up",
				"read")
		case readErrorRate > 0 && readUsageRate > 0:
			// Queries are causing read throttling on the table -> scale up
			expected.ProvisionedRead = scaleUp(
				current.Name,
				current.ProvisionedRead,
				expected.ReadScale.MaxCapacity,
				computeScaleUp(current.ProvisionedRead, expected.ReadScale.MaxCapacity, m.cfg.ScaleUpFactor),
				m.tableReadLastUpdated, expected.ReadScale.OutCooldown,
				"table is in use and there are read throttle errors, scale up",
				"read")
		case readErrorRate == 0 && readUsageRate == 0:
			// this table is not being used. -> scale down
			expected.ProvisionedRead = scaleDown(current.Name,
				current.ProvisionedRead,
				expected.ReadScale.MinCapacity,
				computeScaleDown(current.Name, m.usageReadRates, expected.ReadScale.TargetValue),
				m.tableReadLastUpdated,
				expected.ReadScale.InCooldown,
				"table is not in use. scale down", "read",
				nil)
		}
	}

	return nil
}

func computeScaleUp(currentValue, maxValue int64, scaleFactor float64) int64 {
	scaleUp := int64(float64(currentValue) * scaleFactor)
	// Scale up minimum of 10% of max capacity, to avoid futzing around at low levels
	minIncrement := maxValue / 10
	if scaleUp < currentValue+minIncrement {
		scaleUp = currentValue + minIncrement
	}
	return scaleUp
}

func computeScaleDown(currentName string, usageRates map[string]float64, targetValue float64) int64 {
	usageRate := usageRates[currentName]
	return int64(usageRate * 100.0 / targetValue)
}

func scaleDown(tableName string, currentValue, minValue int64, newValue int64, lastUpdated map[string]time.Time, coolDown int64, msg, operation string, usageRates map[string]float64) int64 {
	if newValue < minValue {
		newValue = minValue
	}
	// If we're already at or below the requested value, it's not a scale-down.
	if newValue >= currentValue {
		return currentValue
	}

	earliest := lastUpdated[tableName].Add(time.Duration(coolDown) * time.Second)
	if earliest.After(mtime.Now()) {
		level.Info(util.Logger).Log("msg", "deferring "+msg, "table", tableName, "till", earliest, "op", operation)
		return currentValue
	}

	// Reject a change that is less than 20% - AWS rate-limits scale-downs so save
	// our chances until it makes a bigger difference
	if newValue > currentValue*4/5 {
		level.Info(util.Logger).Log("msg", "rejected de minimis "+msg, "table", tableName, "current", currentValue, "proposed", newValue, "op", operation)
		return currentValue
	}

	if usageRates != nil {
		// Check that the ingesters seem to be doing some work - don't want to scale down
		// if all our metrics are returning zero, or all the ingesters have crashed, etc
		totalUsage := 0.0
		for _, u := range usageRates {
			totalUsage += u
		}
		if totalUsage < minUsageForScaledown {
			level.Info(util.Logger).Log("msg", "rejected low usage "+msg, "table", tableName, "totalUsage", totalUsage, "op", operation)
			return currentValue
		}
	}

	level.Info(util.Logger).Log("msg", msg, "table", tableName, operation, newValue)
	lastUpdated[tableName] = mtime.Now()
	return newValue
}

func scaleUp(tableName string, currentValue, maxValue int64, newValue int64, lastUpdated map[string]time.Time, coolDown int64, msg, operation string) int64 {
	if newValue > maxValue {
		newValue = maxValue
	}
	earliest := lastUpdated[tableName].Add(time.Duration(coolDown) * time.Second)
	if !earliest.After(mtime.Now()) && newValue > currentValue {
		level.Info(util.Logger).Log("msg", msg, "table", tableName, operation, newValue)
		lastUpdated[tableName] = mtime.Now()
		return newValue
	}

	level.Info(util.Logger).Log("msg", "deferring "+msg, "table", tableName, "till", earliest)
	return currentValue
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

	deMatrix, err := promQuery(ctx, m.promAPI, m.cfg.ThrottleQuery, 0, time.Second)
	if err != nil {
		return err
	}
	if m.throttleRates, err = extractRates(deMatrix); err != nil {
		return err
	}

	usageMatrix, err := promQuery(ctx, m.promAPI, m.cfg.UsageQuery, 0, time.Second)
	if err != nil {
		return err
	}
	if m.usageRates, err = extractRates(usageMatrix); err != nil {
		return err
	}

	readUsageMatrix, err := promQuery(ctx, m.promAPI, m.cfg.ReadUsageQuery, 0, time.Second)
	if err != nil {
		return err
	}
	if m.usageReadRates, err = extractRates(readUsageMatrix); err != nil {
		return err
	}

	readErrorMatrix, err := promQuery(ctx, m.promAPI, m.cfg.ReadErrorQuery, 0, time.Second)
	if err != nil {
		return err
	}
	if m.readErrorRates, err = extractRates(readErrorMatrix); err != nil {
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

	value, wrngs, err := promAPI.QueryRange(ctx, query, queryRange)
	if err != nil {
		return nil, err
	}
	if wrngs != nil {
		level.Warn(util.Logger).Log(
			"query", query,
			"start", queryRange.Start,
			"end", queryRange.End,
			"step", queryRange.Step,
			"warnings", wrngs,
		)
	}
	matrix, ok := value.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("Unable to convert value to matrix: %#v", value)
	}
	return matrix, nil
}
