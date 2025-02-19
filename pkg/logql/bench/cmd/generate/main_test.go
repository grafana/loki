package main

import (
	"context"
	"testing"

	"github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	tenantID := "test-tenant"
	size := int64(1073741824)

	store, err := bench.NewDataObjStore(dir, tenantID)
	require.NoError(t, err)

	builder := bench.NewBuilder(store, bench.DefaultOpt())

	ctx := context.Background()
	if err := builder.Generate(ctx, size); err != nil {
		t.Fatalf("Failed to generate dataset: %v", err)
	}
}
