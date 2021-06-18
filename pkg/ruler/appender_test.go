package ruler

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/validation"
)

var (
	logger            = log.NewNopLogger()
	fakeUserID        = "fake"
	emptyWriteRequest = []byte{}
	queueCapacity     = 10
	metrics           = newRemoteWriteMetrics(prometheus.DefaultRegisterer)
)

func TestGroupKeyRetrieval(t *testing.T) {
	ruleFile := "/my/file"
	groupName := "my-group"

	ctx := createOriginContext(ruleFile, groupName)
	// group key should match value derived from context
	require.Equal(t, rules.GroupKey(ruleFile, groupName), retrieveGroupKeyFromContext(ctx))

	// group key should be blank if context does not contain expected data
	require.Equal(t, "", retrieveGroupKeyFromContext(context.TODO()))
}

// TestMemoizedAppenders tests that appenders are memoized by their associated group key
func TestMemoizedAppenders(t *testing.T) {
	ctx := createOriginContext("/rule/file", "rule-group")
	appendable := createBasicAppendable(queueCapacity)

	// context passing a valid group key will allow the appender to be memoized
	appender := appendable.Appender(ctx)
	require.Same(t, appender, appendable.Appender(ctx))

	// a missing or invalid group key will force a new appender to be created each time
	ctx = promql.NewOriginContext(context.TODO(), nil)
	appender = appendable.Appender(ctx)
	require.NotSame(t, appender, appendable.Appender(ctx))
}

// TestMemoizedAppendersWithRuntimeCapacityChange tests that memoized appenders can reconfigure their capacity
func TestMemoizedAppendersWithRuntimeCapacityChange(t *testing.T) {
	ctx := createOriginContext("/rule/file", "rule-group")
	appendable := createBasicAppendable(queueCapacity)

	appender := appendable.Appender(ctx)

	// appender is configured with default queue capacity initially
	capacity := appender.(*RemoteWriteAppender).queue.Capacity()
	require.Equal(t, queueCapacity, capacity)

	newCapacity := 123

	// reconfigure the overrides to simulate a runtime config change
	appendable.overrides = fakeLimits(newCapacity)

	// appender is reconfigured with new queue capacity when retrieved again
	appender = appendable.Appender(ctx)
	capacity = appender.(*RemoteWriteAppender).queue.Capacity()
	require.Equal(t, newCapacity, capacity)
}

func TestAppenderSeparationByRuleGroup(t *testing.T) {
	ctxA := createOriginContext("/rule/fileA", "rule-groupA")
	ctxB := createOriginContext("/rule/fileB", "rule-groupB")
	appendable := createBasicAppendable(queueCapacity)

	appenderA := appendable.Appender(ctxA)
	appenderB := appendable.Appender(ctxB)
	require.NotSame(t, appenderA, appenderB)
}

func TestQueueCapacity(t *testing.T) {
	ctx := createOriginContext("/rule/file", "rule-group")
	appendable := createBasicAppendable(queueCapacity)

	appender := appendable.Appender(ctx).(*RemoteWriteAppender)
	require.Equal(t, appender.queue.Capacity(), queueCapacity)
}

func TestQueueCapacityTenantOverride(t *testing.T) {
	ctx := createOriginContext("/rule/file", "rule-group")
	appendable := createBasicAppendable(queueCapacity)

	overriddenCapacity := 999
	overrides, err := validation.NewOverrides(validation.Limits{}, func(userID string) *validation.Limits {
		return &validation.Limits{
			RulerRemoteWriteQueueCapacity: overriddenCapacity,
		}
	})
	require.Nil(t, err)
	appendable.overrides = overrides

	appender := appendable.Appender(ctx).(*RemoteWriteAppender)
	require.Equal(t, appender.queue.Capacity(), overriddenCapacity)
}

func TestAppendSample(t *testing.T) {
	ctx := createOriginContext("/rule/file", "rule-group")
	appendable := createBasicAppendable(queueCapacity)
	appender := appendable.Appender(ctx).(*RemoteWriteAppender)

	labels := labels.Labels{
		labels.Label{
			Name:  "cluster",
			Value: "us-central1",
		},
	}
	ts := time.Now().Unix()
	val := 91.2

	sample := TimeSeriesEntry{
		Labels: labels,
		Sample: cortexpb.Sample{
			Value:       val,
			TimestampMs: ts,
		},
	}

	_, err := appender.Append(0, labels, ts, val)
	require.Nil(t, err)

	require.Equal(t, appender.queue.Entries()[0], sample)
}

func TestSuccessfulRemoteWriteSample(t *testing.T) {
	client := &MockRemoteWriteClient{}

	appendable := createBasicAppendable(queueCapacity)

	appender := appendable.Appender(context.TODO()).(*RemoteWriteAppender)
	appender.remoteWriter = client

	client.On("PrepareRequest", mock.Anything).Return(emptyWriteRequest, nil).Once()
	client.On("Store", mock.Anything, mock.Anything).Return(nil).Once()

	_, err := appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.2)
	require.Nil(t, err)

	// commit didn't return any error, which means a successful write
	err = appender.Commit()
	require.Nil(t, err)

	// queue should be cleared on successful write
	require.Zero(t, appender.queue.Length())

	client.AssertExpectations(t)
}

func TestUnsuccessfulRemoteWritePrepare(t *testing.T) {
	client := &MockRemoteWriteClient{}

	appendable := createBasicAppendable(queueCapacity)

	appender := appendable.Appender(context.TODO()).(*RemoteWriteAppender)
	appender.remoteWriter = client

	client.On("PrepareRequest", mock.Anything).Return(emptyWriteRequest, fmt.Errorf("some error")).Once()
	_, err := appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.2)
	require.Nil(t, err)

	// commit fails if PrepareRequest returns an error
	err = appender.Commit()
	require.NotNil(t, err)

	// queue should NOT be cleared on unsuccessful write
	require.NotZero(t, appender.queue.Length())

	client.AssertExpectations(t)
}

func TestUnsuccessfulRemoteWriteStore(t *testing.T) {
	client := &MockRemoteWriteClient{}

	appendable := createBasicAppendable(queueCapacity)

	appender := appendable.Appender(context.TODO()).(*RemoteWriteAppender)
	appender.remoteWriter = client

	client.On("PrepareRequest", mock.Anything).Return(emptyWriteRequest, nil).Once()
	client.On("Store", mock.Anything, mock.Anything).Return(fmt.Errorf("some error")).Once()
	_, err := appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.2)
	require.Nil(t, err)

	// commit fails if Store returns an error
	err = appender.Commit()
	require.NotNil(t, err)

	// queue should NOT be cleared on unsuccessful write
	require.NotZero(t, appender.queue.Length())

	client.AssertExpectations(t)
}

func TestEmptyRemoteWrite(t *testing.T) {
	client := &MockRemoteWriteClient{}

	appendable := createBasicAppendable(queueCapacity)
	appender := appendable.Appender(context.TODO()).(*RemoteWriteAppender)
	appender.remoteWriter = client

	// queue should be empty
	require.Zero(t, appender.queue.Length())

	// no error returned
	err := appender.Commit()
	require.Nil(t, err)

	// PrepareRequest & Store were not called either
	client.AssertExpectations(t)
}

func TestAppenderRollback(t *testing.T) {
	appendable := createBasicAppendable(queueCapacity)
	appender := appendable.Appender(context.TODO()).(*RemoteWriteAppender)

	appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.2) //nolint:errcheck
	appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.2) //nolint:errcheck
	appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.2) //nolint:errcheck

	require.Equal(t, 3, appender.queue.Length())

	require.Nil(t, appender.Rollback())
	require.Zero(t, appender.queue.Length())
}

func TestAppenderEvictOldest(t *testing.T) {
	capacity := 2
	appendable := createBasicAppendable(capacity)

	appender := appendable.Appender(context.TODO()).(*RemoteWriteAppender)

	appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.2) //nolint:errcheck
	appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.3) //nolint:errcheck
	appender.Append(0, labels.Labels{}, time.Now().UnixNano(), 11.4) //nolint:errcheck

	// capacity is enforced
	require.Equal(t, capacity, appender.queue.Length())

	// only two newest samples are kept
	require.Equal(t, appender.queue.Entries()[0].(TimeSeriesEntry).Sample.Value, 11.3)
	require.Equal(t, appender.queue.Entries()[1].(TimeSeriesEntry).Sample.Value, 11.4)
}

// context is created by ruler, passing along details of the rule being executed
// see github.com/prometheus/prometheus/rules/manager.go
// 	-> func (g *Group) run(ctx context.Context)
func createOriginContext(ruleFile, groupName string) context.Context {
	return promql.NewOriginContext(context.TODO(), map[string]interface{}{
		"ruleGroup": map[string]string{
			"file": ruleFile,
			"name": groupName,
		},
	})
}

func createBasicAppendable(queueCapacity int) *RemoteWriteAppendable {
	target, err := url.Parse("http://some/target")
	if err != nil {
		panic(err)
	}

	return newRemoteWriteAppendable(
		Config{
			RemoteWrite: RemoteWriteConfig{
				Enabled: true,
				Client: config.RemoteWriteConfig{
					URL: &promConfig.URL{URL: target},
				},
			},
		},
		fakeLimits(queueCapacity),
		logger,
		fakeUserID,
		metrics,
	)
}

func fakeLimits(queueCapacity int) RulesLimits {
	o, err := validation.NewOverrides(validation.Limits{
		RulerRemoteWriteQueueCapacity: queueCapacity,
	}, nil)
	if err != nil {
		panic(err)
	}
	return o
}

type MockRemoteWriteClient struct {
	mock.Mock
}

// Store stores the given samples in the remote storage.
func (c *MockRemoteWriteClient) Store(ctx context.Context, data []byte) error {
	args := c.Called(ctx, data)
	return args.Error(0)
}

// Name uniquely identifies the remote storage.
func (c *MockRemoteWriteClient) Name() string { return "" }

// Endpoint is the remote read or write endpoint for the storage client.
func (c *MockRemoteWriteClient) Endpoint() string { return "" }

func (c *MockRemoteWriteClient) PrepareRequest(queue util.Queue) ([]byte, error) {
	args := c.Called(queue)
	return args.Get(0).([]byte), args.Error(1)
}
