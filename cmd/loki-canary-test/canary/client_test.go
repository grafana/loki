package canary_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/cmd/loki-canary-test/canary"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

const sumEntriesQuery = "sum(loki_canary_entries_total)"
const sumEntriesMissingQuery = "sum(loki_canary_missing_entries_total)"

func TestCanary(t *testing.T) {
	testContext := func(query func(string) (model.Value, v1.Warnings, error)) (*canary.Client, *mockAPI) {
		api := &mockAPI{
			query: query,
		}

		client := canary.NewCanaryClient(
			"http://localhost:9090",
			api,
			time.Millisecond,
		)

		return client, api
	}

	t.Run("Run queries total entries", func(t *testing.T) {
		client, api := testContext(func(query string) (model.Value, v1.Warnings, error) {
			return model.Vector{}, nil, nil
		})

		err := client.Run(1)
		assert.NoError(t, err)

		assert.GreaterOrEqual(t, len(api.queries), 1)
		assert.Contains(t, api.queries, "sum(loki_canary_entries_total)")
	})

	t.Run("Run queries total missing entries", func(t *testing.T) {
		client, api := testContext(func(query string) (model.Value, v1.Warnings, error) {
			return model.Vector{}, nil, nil
		})

		err := client.Run(1)
		assert.NoError(t, err)

		assert.GreaterOrEqual(t, len(api.queries), 1)
		assert.Contains(t, api.queries, "sum(loki_canary_missing_entries_total)")
	})

	t.Run("Run retries up to retry count on error", func(t *testing.T) {
		client, api := testContext(func(query string) (model.Value, v1.Warnings, error) {
			return nil, nil, fmt.Errorf("error")
		})

		err := client.Run(3)
		assert.Error(t, err)

		assert.Equal(t, len(api.queries), 3, "should error after first query attempt, resulting in 3 queries attempted")
	})

	t.Run("Run does not retry on success", func(t *testing.T) {
		client, api := testContext(func(query string) (model.Value, v1.Warnings, error) {
			return model.Vector{}, nil, nil
		})

		err := client.Run(3)
		assert.NoError(t, err)

		assert.Equal(t, len(api.queries), 2, "both queries should succeed, resulting in 2 queries attempted")
	})

	t.Run("Run expects there to be more than 0 log entries produced by the canary", func(t *testing.T) {
		client, _ := testContext(func(query string) (model.Value, v1.Warnings, error) {
			if query == sumEntriesQuery {
				return model.Vector{
					&model.Sample{
						Value: 0,
					},
				}, nil, nil
			}

			if query == sumEntriesMissingQuery {
				return model.Vector{
					&model.Sample{
						Value: 0,
					},
				}, nil, nil
			}

			return model.Vector{}, nil, nil
		})

		err := client.Run(1)
		assert.Error(t, err)

		client, _ = testContext(func(query string) (model.Value, v1.Warnings, error) {
			if query == sumEntriesQuery {
				return model.Vector{
					&model.Sample{
						Value: 100,
					},
				}, nil, nil
			}

			if query == sumEntriesMissingQuery {
				return model.Vector{
					&model.Sample{
						Value: 0,
					},
				}, nil, nil
			}

			return model.Vector{}, nil, nil
		})

		err = client.Run(1)
		assert.NoError(t, err)
	})

	t.Run("Run expects there to be no log entries missing", func(t *testing.T) {
		client, _ := testContext(func(query string) (model.Value, v1.Warnings, error) {
			if query == sumEntriesQuery {
				return model.Vector{
					&model.Sample{
						Value: 100,
					},
				}, nil, nil
			}

			if query == sumEntriesMissingQuery {
				return model.Vector{
					&model.Sample{
						Value: 100,
					},
				}, nil, nil
			}

			return model.Vector{}, nil, nil
		})

		err := client.Run(1)
		assert.Error(t, err)

		client, _ = testContext(func(query string) (model.Value, v1.Warnings, error) {
			if query == sumEntriesQuery {
				return model.Vector{
					&model.Sample{
						Value: 100,
					},
				}, nil, nil
			}

			if query == sumEntriesMissingQuery {
				return model.Vector{
					&model.Sample{
						Value: 0,
					},
				}, nil, nil
			}

			return model.Vector{}, nil, nil
		})

		err = client.Run(1)
		assert.NoError(t, err)
	})
}

type mockAPI struct {
	queries []string
	query   func(string) (model.Value, v1.Warnings, error)
}

// Query performs a query for the given time.
func (m *mockAPI) Query(ctx context.Context, query string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
	m.queries = append(m.queries, query)
	return m.query(query)
}

// ---- Unused methods ----

// Alerts returns a list of all active alerts.
func (m *mockAPI) Alerts(ctx context.Context) (v1.AlertsResult, error) {
	panic("not implemented") // TODO: Implement
}

// AlertManagers returns an overview of the current state of the Prometheus alert manager discovery.
func (m *mockAPI) AlertManagers(ctx context.Context) (v1.AlertManagersResult, error) {
	panic("not implemented") // TODO: Implement
}

// CleanTombstones removes the deleted data from disk and cleans up the existing tombstones.
func (m *mockAPI) CleanTombstones(ctx context.Context) error {
	panic("not implemented") // TODO: Implement
}

// Config returns the current Prometheus configuration.
func (m *mockAPI) Config(ctx context.Context) (v1.ConfigResult, error) {
	panic("not implemented") // TODO: Implement
}

// DeleteSeries deletes data for a selection of series in a time range.
func (m *mockAPI) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	panic("not implemented") // TODO: Implement
}

// Flags returns the flag values that Prometheus was launched with.
func (m *mockAPI) Flags(ctx context.Context) (v1.FlagsResult, error) {
	panic("not implemented") // TODO: Implement
}

// LabelNames returns the unique label names present in the block in sorted order by given time range and matchers.
func (m *mockAPI) LabelNames(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	panic("not implemented") // TODO: Implement
}

// LabelValues performs a query for the values of the given label, time range and matchers.
func (m *mockAPI) LabelValues(ctx context.Context, label string, matches []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	panic("not implemented") // TODO: Implement
}

// QueryRange performs a query for the given range.
func (m *mockAPI) QueryRange(ctx context.Context, query string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
	panic("not implemented") // TODO: Implement
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (m *mockAPI) QueryExemplars(ctx context.Context, query string, startTime time.Time, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	panic("not implemented") // TODO: Implement
}

// Buildinfo returns various build information properties about the Prometheus server
func (m *mockAPI) Buildinfo(ctx context.Context) (v1.BuildinfoResult, error) {
	panic("not implemented") // TODO: Implement
}

// Runtimeinfo returns the various runtime information properties about the Prometheus server.
func (m *mockAPI) Runtimeinfo(ctx context.Context) (v1.RuntimeinfoResult, error) {
	panic("not implemented") // TODO: Implement
}

// Series finds series by label matchers.
func (m *mockAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	panic("not implemented") // TODO: Implement
}

// Snapshot creates a snapshot of all current data into snapshots/<datetime>-<rand>
// under the TSDB's data directory and returns the directory as response.
func (m *mockAPI) Snapshot(ctx context.Context, skipHead bool) (v1.SnapshotResult, error) {
	panic("not implemented") // TODO: Implement
}

// Rules returns a list of alerting and recording rules that are currently loaded.
func (m *mockAPI) Rules(ctx context.Context) (v1.RulesResult, error) {
	panic("not implemented") // TODO: Implement
}

// Targets returns an overview of the current state of the Prometheus target discovery.
func (m *mockAPI) Targets(ctx context.Context) (v1.TargetsResult, error) {
	panic("not implemented") // TODO: Implement
}

// TargetsMetadata returns metadata about metrics currently scraped by the target.
func (m *mockAPI) TargetsMetadata(ctx context.Context, matchTarget string, metric string, limit string) ([]v1.MetricMetadata, error) {
	panic("not implemented") // TODO: Implement
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (m *mockAPI) Metadata(ctx context.Context, metric string, limit string) (map[string][]v1.Metadata, error) {
	panic("not implemented") // TODO: Implement
}

// TSDB returns the cardinality statistics.
func (m *mockAPI) TSDB(ctx context.Context) (v1.TSDBResult, error) {
	panic("not implemented") // TODO: Implement
}

// WalReplay returns the current replay status of the wal.
func (m *mockAPI) WalReplay(ctx context.Context) (v1.WalReplayStatus, error) {
	panic("not implemented") // TODO: Implement
}
