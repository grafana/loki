package bench

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// DataObjV2EngineStore uses the new pkg/engine/engine.New for querying.
// It assumes the engine can read the "new dataobj format" (e.g. Parquet)
// from the provided dataDir via a filesystem objstore.Bucket.
type DataObjV2EngineStore struct {
	engine   logql.Engine // Use the interface type
	tenantID string
	dataDir  string
}

// NewDataObjV2EngineStore creates a new store that uses the v2 dataobj engine.
func NewDataObjV2EngineStore(dataDir string, tenantID string) (*DataObjV2EngineStore, error) {
	logger := log.NewNopLogger()

	// Setup filesystem client as objstore.Bucket
	// This assumes the engine is configured to read its specific data format (e.g., Parquet)
	// from this directory structure. The existing benchmark data generation might need adjustments
	// if it doesn't produce this format.
	// Use NewBucket from thanos-io/objstore/providers/filesystem, similar to store_dataobj.go
	bucketClient, err := filesystem.NewBucket(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem bucket for DataObjV2EngineStore: %w", err)
	}

	// Default EngineOpts. Adjust if specific configurations are needed.
	engineOpts := logql.EngineOpts{
		EnableV2Engine: true,
	}

	// Instantiate the new engine
	// Note: The tenantID is not directly passed to engine.New here.
	// The engine might expect tenant information to be part of the query context
	// or derived from the bucket structure if it's multi-tenant aware.
	// This might require adjustment based on how pkg/engine/engine actually handles multi-tenancy
	// with a generic objstore.Bucket.
	queryEngine := engine.New(engineOpts, bucketClient, logql.NoLimits, nil, logger)

	return &DataObjV2EngineStore{
		engine:   queryEngine,
		tenantID: tenantID, // Store for context or if querier needs it
		dataDir:  dataDir,
	}, nil
}

// Querier returns a logql.Querier for the DataObjV2EngineStore.
func (s *DataObjV2EngineStore) Querier() (logql.Querier, error) {
	return &dataObjV2EngineQuerier{
		engine:   s.engine,
		tenantID: s.tenantID, // Pass tenantID if SelectLogs needs it for context
	}, nil
}

type dataObjV2EngineQuerier struct {
	engine   logql.Engine
	tenantID string
}

// SelectLogs implements logql.Querier.
func (q *dataObjV2EngineQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	// Construct logql.Params from logql.SelectLogParams
	// The logql.SelectLogParams.Query is the full LogQL query string.
	logqlParams, err := logql.NewLiteralParams(
		params.QueryRequest.Plan.String(), // Assuming this is the correct way to get the full query string
		params.Start,
		params.End,
		0,
		0,
		params.Direction,
		params.Limit,
		nil,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("DataObjV2EngineStore failed to create literal params: %w", err)
	}

	// Inject tenantID into context if required by the engine.
	// This is a common pattern.
	// ctx = user.InjectOrgID(ctx, q.tenantID) // If using dskit/user for tenant context

	// Execute query
	compiledQuery := q.engine.Query(logqlParams)
	result, err := compiledQuery.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("DataObjV2EngineStore query execution failed: %w", err)
	}

	// Convert result (logqlmodel.Streams) to iter.EntryIterator
	switch data := result.Data.(type) {
	case logqlmodel.Streams:
		return newStreamsEntryIterator(data, params.Direction), nil // Pass direction
	default:
		return nil, fmt.Errorf("DataObjV2EngineStore: unexpected result type for SelectLogs: %T", result.Data)
	}
}

// SelectSamples implements logql.Querier.
func (q *dataObjV2EngineQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	return nil, fmt.Errorf("SelectSamples not implemented for DataObjV2EngineStore")
}

// newStreamsEntryIterator creates a sorted entry iterator from multiple logqlmodel.Streams.
func newStreamsEntryIterator(streams logqlmodel.Streams, direction logproto.Direction) iter.EntryIterator {
	iterators := make([]iter.EntryIterator, 0, len(streams))
	for _, stream := range streams {
		if len(stream.Entries) > 0 {
			iterators = append(iterators, iter.NewStreamIterator(stream))
		}
	}
	return iter.NewSortEntryIterator(iterators, direction)
}
