package bench

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/logql"
)

var errStoreUnimplemented = errors.New("store does not implement this operation")

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
	storeDir := filepath.Join(dataDir, "dataobj")
	bucketClient, err := filesystem.NewBucket(storeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem bucket for DataObjV2EngineStore: %w", err)
	}

	// Default EngineOpts. Adjust if specific configurations are needed.
	engineOpts := logql.EngineOpts{
		EnableV2Engine: true,
		BatchSize:      512,
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
