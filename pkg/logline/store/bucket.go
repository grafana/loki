package store

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// DefaultClientName is the object-storage client name reported in metrics when
// no explicit name is given.
const DefaultClientName = "logline-objstore"

// New validates cfg, builds the object-storage bucket described by the schema
// and object-store configs, and returns a Store backed by it.
//
// Config is validated before any object storage is touched so that a bad
// configuration fails without a network round trip.
func New(
	ctx context.Context,
	schemaCfg config.SchemaConfig,
	objectStoreCfg bucket.ConfigWithNamedStores,
	cfg Config,
	logger log.Logger,
	reg prometheus.Registerer,
) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid store config: %w", err)
	}

	bkt, err := NewBucket(ctx, schemaCfg, objectStoreCfg, cfg.BucketPrefix, DefaultClientName, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create object storage bucket: %w", err)
	}

	return NewStore(bkt, cfg, logger, reg)
}

// NewBucket creates an objstore.Bucket for the backend named by the schema
// period covering now, with all reads and writes confined to prefix.
//
// Named stores are resolved first so a schema that points at a named store gets
// that store's backend and settings rather than the top-level ones.
func NewBucket(
	ctx context.Context,
	schemaCfg config.SchemaConfig,
	objectStoreCfg bucket.ConfigWithNamedStores,
	prefix string,
	clientName string,
	logger log.Logger,
) (objstore.Bucket, error) {
	level.Info(logger).Log("msg", "initializing object storage bucket", "prefix", prefix)

	schema, err := schemaCfg.SchemaForTime(model.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to get schema for now: %w", err)
	}

	backend := schema.ObjectType
	if st, ok := objectStoreCfg.NamedStores.LookupStoreType(schema.ObjectType); ok {
		backend = st
		if err := objectStoreCfg.NamedStores.OverrideConfig(&objectStoreCfg.Config, schema.ObjectType); err != nil {
			return nil, err
		}
	}

	if clientName == "" {
		clientName = DefaultClientName
	}

	var bkt objstore.Bucket
	bkt, err = bucket.NewClient(ctx, backend, objectStoreCfg.Config, clientName, logger, nil)
	if err != nil {
		return nil, err
	}

	bkt = objstore.NewPrefixedBucket(bkt, prefix)

	// Smoke-test the bucket so a misconfigured backend fails at startup rather
	// than on the first query.
	if err := bkt.Iter(ctx, "", func(_ string) error { return nil }); err != nil {
		return nil, fmt.Errorf("failed to list object store bucket: %w", err)
	}

	return bkt, nil
}
